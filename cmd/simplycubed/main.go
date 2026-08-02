// Command simplycubed is the CLI for SimplyCubed Code. It runs the same engine
// the GitHub Action runs, locally, for development and debugging.
//
//	simplycubed version
//	simplycubed run <owner/repo#N> [flags]
//
// This is an early scaffold. See STATUS.md.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/simplycubed/code/internal/app"
	"github.com/simplycubed/code/internal/buildinfo"
	"github.com/simplycubed/code/internal/command"
	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine"
	"github.com/simplycubed/code/internal/engine/claude"
	"github.com/simplycubed/code/internal/engine/codex"
	forge2 "github.com/simplycubed/code/internal/forge"
	"github.com/simplycubed/code/internal/forge/dryrun"
	forgegh "github.com/simplycubed/code/internal/forge/gh"
	"github.com/simplycubed/code/internal/loop"
	vcsgit "github.com/simplycubed/code/internal/vcs/git"
	"github.com/simplycubed/code/internal/worktree"
)

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch runs one command and returns the process exit code. main is only a
// wrapper around it, so the whole entry point (argument handling, exit codes,
// error reporting) is reachable from a test instead of being the one part of
// the CLI that nothing exercises.
func dispatch(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	fail := func(err error) int {
		fmt.Fprintln(stderr, "error:", err)
		// A missing adopter-set value is something to go and set, not a bug to
		// report. Its own exit code lets a caller tell those apart without
		// matching on message text.
		if errors.Is(err, ErrConfigMissing) {
			return 3
		}
		return 1
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, buildinfo.Version)
	case "init":
		if err := initCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "command":
		if err := commandCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "preflight":
		if err := preflightCmd(args[1:], stdout); err != nil {
			return fail(err)
		}
	case "run":
		if err := runCmd(args[1:]); err != nil {
			return fail(err)
		}
	case "address":
		if err := addressCmd(args[1:]); err != nil {
			return fail(err)
		}
	default:
		usage(stderr)
		return 2
	}
	return 0
}

func usage(w io.Writer) {
	fmt.Fprintf(w, `simplycubed %s

usage:
  simplycubed version
  simplycubed init [--repo-dir .] [--workflow]
  simplycubed preflight [--repo-dir .]
  simplycubed command <owner/repo#N> --body "<comment>" [flags]
  simplycubed run <owner/repo#N> [flags]
  simplycubed address <owner/repo#PR> [flags]

run drives an issue to a pull request. address runs the fix-on-request loop
over an open pull request: it reads the human's review feedback and pushes a
fix back to the same branch, or reports that there is nothing new to address.

flags (both commands):
  --repo-dir    path to the target repo checkout (default ".")
  --actor       login that triggered the run; refused unless it has write access
  --dry-run     run the whole loop but make no GitHub writes and no push
  --model       engine model/deployment name (default "gpt-5.4")
  --base        worktree base ref (default "origin/HEAD")
  --state-dir   where worktrees and the generated config live

environment (engine settings; never committed to a repo):
  SIMPLYCUBED_AZURE_OPENAI_ENDPOINT   e.g. https://<resource>.openai.azure.com
  SIMPLYCUBED_AZURE_OPENAI_API_KEY    the key, read by name and never written to disk
`, buildinfo.Version)
}

const starterConfig = `# Repository-local contract for SimplyCubed Code.
# Keep the label prefix unless you already run another sc:* lifecycle.
labelPrefix: sc

# The GitHub App you created and installed, without the "[bot]" suffix. Comment
# commands address it: "@__SIMPLYCUBED_APP_NAME__ go" on an issue starts work.
#
# This is yours, not ours. App names are globally unique, so every installation
# has a different one, and addressing your real bot is what makes GitHub offer
# it in the autocomplete after someone types "@".
#
# The caller workflow triggers on this same handle. If you rename the App,
# change it here and re-run "simplycubed init --workflow"; preflight fails if
# the two ever disagree.
appName: __SIMPLYCUBED_APP_NAME__

# Required. Fill this in with the real gate that is already green on main.
gate:

# Optional. One-time setup command before each run (install deps, generate code).
# setup:

# Optional. Disable the generated commit/PR attribution marker.
# attribution: false

# Optional. Ask the agent to add a generated walkthrough and changes table to PRs.
# prDescription: rich
`

const (
	latestKnownWorkflowTag     = "v0.2.0"
	callerWorkflowTagToken     = "__SIMPLYCUBED_TAG__"
	callerWorkflowAppNameToken = "__SIMPLYCUBED_APP_NAME__"
)

//go:embed simplycubed-caller.yml.tmpl
var callerWorkflowTemplate string

//go:embed simplycubed-selftest.yml.tmpl
var selftestWorkflowTemplate string

// engineEnv validates the selected engine settings and returns the normalized
// Azure endpoint when the engine needs one. It is the single implementation:
// `prepare` calls it before a run, and `preflight` calls it so a workflow can
// fail early without a second copy of these rules written in shell.
func engineEnv(cfg *config.Config) (string, error) {
	if cfg != nil && cfg.Engine == "claude" {
		return "", nil
	}
	if err := requireSet("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", sectionVariable); err != nil {
		return "", err
	}
	endpoint := strings.TrimRight(os.Getenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT"), "/")
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT is not a valid URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT must be an https URL like https://<resource>.openai.azure.com, got %q", endpoint)
	}
	if err := requireSet("SIMPLYCUBED_AZURE_OPENAI_API_KEY", sectionSecret); err != nil {
		return "", err
	}
	return endpoint, nil
}

// newVCS builds the git layer with a committer identity. A GitHub Actions
// runner has none configured, so without this a commit fails with "Author
// identity unknown" after the change is already made and the gate has passed.
// The identity is the credential's own login, so commits are attributable to
// whoever the run authenticated as.
// ErrConfigMissing marks a missing adopter-set value, as opposed to an internal
// failure. The two want different responses: one is something to go and set,
// the other is a bug. dispatch turns this into its own exit code so a caller
// can tell them apart without parsing text.
var ErrConfigMissing = errors.New("configuration missing")

// section is where a value lives in the adopter's repository settings. GitHub
// puts Variables and Secrets on different tabs, and a value filed under the
// wrong one reads back as empty rather than failing, so every message about a
// missing value has to say which tab it belongs on.
type section string

const (
	sectionVariable section = "variable"
	sectionSecret   section = "secret"
)

// requireSet returns an error naming the value and its section when unset or
// empty. Empty matters as much as unset: an empty string is exactly what a
// value filed under the wrong tab looks like from here.
func requireSet(name string, where section) error {
	if strings.TrimSpace(os.Getenv(name)) != "" {
		return nil
	}
	return fmt.Errorf("%w: %s is not set. It is a repository %s on your own repository, under Settings > Secrets and variables > Actions; a reusable workflow never inherits %ss from SimplyCubed", ErrConfigMissing, name, where, where)
}

func newVCS(self string) *vcsgit.Git {
	if self == "" {
		return &vcsgit.Git{}
	}
	return &vcsgit.Git{
		AuthorName:  self,
		AuthorEmail: fmt.Sprintf("%s@users.noreply.github.com", strings.ReplaceAll(self, "[bot]", "")),
	}
}

// newRunner builds the engine runner, honouring SIMPLYCUBED_SANDBOX.
//
// The sandbox stays on. Nothing in this repository widens it, and the knob
// exists only so an adopter who has genuinely sandboxed their runners
// externally can decide that for themselves.
//
// The default sandbox uses bubblewrap on Linux, which needs an unprivileged
// user namespace. Ubuntu 24.04 restricts those by AppArmor, so on a stock
// GitHub runner every command the engine tried died at startup:
//
//	bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted
//
// The engine then read nothing and changed nothing while still exiting
// successfully, which is why it looked like a silent no-op. The reusable
// workflow now enables that kernel feature before the engine runs, which lets
// the sandbox start rather than widening it. A run that still cannot sandbox
// stops and a human finishes the work; it never falls back to running
// unconfined. See docs/faq.md and the self-test.
func newRunner(cfg *config.Config, codexHome string) engine.Runner {
	if cfg.Engine == "claude" {
		return claude.New()
	}
	r := codex.New(codexHome)
	if mode := os.Getenv("SIMPLYCUBED_SANDBOX"); mode != "" {
		r.Sandbox = mode
	}
	return r
}

// takeBody removes --body (in either form) from argv and returns it with the
// remaining arguments.
func takeBody(argv []string) (body string, rest []string, err error) {
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--body" || a == "-body":
			if i+1 >= len(argv) {
				return "", nil, fmt.Errorf("--body requires a value")
			}
			body = argv[i+1]
			i++
		case strings.HasPrefix(a, "--body="), strings.HasPrefix(a, "-body="):
			_, v, _ := strings.Cut(a, "=")
			body = v
		default:
			rest = append(rest, a)
		}
	}
	return body, rest, nil
}

// appNameFor reads the configured App handle from the same --repo-dir the rest
// of the command will use. It parses only that flag, because run and address
// own the full flag set and a second copy of it would drift.
func appNameFor(argv []string) (string, error) {
	repoDir := "."
	for i, a := range argv {
		switch {
		case a == "--repo-dir" || a == "-repo-dir":
			if i+1 < len(argv) {
				repoDir = argv[i+1]
			}
		case strings.HasPrefix(a, "--repo-dir="), strings.HasPrefix(a, "-repo-dir="):
			_, v, _ := strings.Cut(a, "=")
			repoDir = v
		}
	}
	cfg, err := config.Load(filepath.Join(repoDir, ".github", "simplycubed.yml"))
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}
	return cfg.AppName, nil
}

// commandCmd routes a comment addressed to the agent to the matching loop. The
// comment body is untrusted, so it is parsed into a fixed vocabulary here and
// never interpreted: an unrecognised comment does nothing at all.
func commandCmd(argv []string, stdout io.Writer) error {
	// --body is pulled out by hand and everything else is forwarded untouched,
	// because run and address own the rest of the flags. Parsing them here would
	// mean maintaining a second copy of that flag set.
	body, rest, err := takeBody(argv)
	if err != nil {
		return err
	}
	// The handle is per-repository, so the config has to be read before the
	// comment can be parsed at all: which App this repository installed is the
	// thing that decides what a command even looks like here.
	appName, err := appNameFor(rest)
	if err != nil {
		return err
	}
	kind := command.Parse(body, appName)

	switch kind {
	case command.Help:
		return reply(rest, command.HelpTextFor(appName), stdout)
	case command.Go, command.Address:
		// A verb aimed at the wrong surface is answered, not run. This lives on
		// the comment path rather than inside run and address because someone
		// typing `simplycubed address owner/repo#96` at a shell wants an error,
		// not a comment posted in their name.
		a, err := newAnswerer(rest)
		if err != nil {
			return err
		}
		if a.ok && command.Misdirected(kind, a.onPR) {
			return a.post(command.MisdirectedText(kind, a.onPR, appName), stdout)
		}
		if kind == command.Go {
			return runCmd(rest)
		}
		return addressCmd(rest)
	default:
		// Only answer a comment that was actually addressed to the agent.
		// Anything else is someone mentioning it in passing, and replying to
		// that would make it the noisiest participant in every thread.
		if command.Addressed(body, appName) {
			return reply(rest, command.UnknownTextFor(appName), stdout)
		}
		fmt.Fprintln(stdout, "no command recognised in that comment; nothing to do")
		return nil
	}
}

// newCommentForge builds the forge used to answer a comment. It is a variable so
// the routing tests can answer without a network.
var newCommentForge = func() forge2.Forge { return &forgegh.Forge{} }

// answerer is the little that is needed to reply to a comment: which thread it
// arrived on, and something to answer with.
//
// It deliberately does not go through prepare. Answering `help`, or telling
// someone they used the wrong verb, must work in a repository that has no config
// and no engine credentials, because those are exactly the repositories where
// someone is most likely to be asking.
type answerer struct {
	forge forge2.Forge
	dry   *dryrun.Forge
	ref   domain.Issue
	onPR  bool
	// ok is false when the arguments carry no issue ref, which is what a local
	// run driven by hand looks like. There is then nothing to answer on.
	ok bool
}

func newAnswerer(argv []string) (answerer, error) {
	var a answerer
	// The ref is found by shape rather than by position, because the flags it
	// sits among belong to run and address and are not parsed here.
	for _, arg := range argv {
		if ref, err := app.ParseIssueRef(arg); err == nil {
			a.ref, a.ok = ref, true
			break
		}
	}
	if !a.ok {
		return a, nil
	}
	gf := newCommentForge()
	a.forge = gf
	if dryRunRequested(argv) {
		a.dry = dryrun.New(gf)
		a.forge = a.dry
	}
	onPR, err := a.forge.IsPullRequest(context.Background(), a.ref.Repo, a.ref.Number)
	if err != nil {
		return a, fmt.Errorf("resolve %s#%d: %w", a.ref.Repo, a.ref.Number, err)
	}
	a.onPR = onPR
	return a, nil
}

// post answers on the thread and echoes the same text, so a run driven from a
// terminal still shows it where the operator is looking. Under a dry run the
// post is recorded rather than made, like every other GitHub write.
func (a answerer) post(body string, stdout io.Writer) error {
	fmt.Fprintln(stdout, body)
	if !a.ok {
		return nil
	}
	defer func() {
		if a.dry == nil {
			return
		}
		fmt.Fprintln(stdout)
		fmt.Fprintln(stdout, a.dry.Report())
	}()
	ctx := context.Background()
	if a.onPR {
		return a.forge.CommentPR(ctx, a.ref.Repo, a.ref.Number, body)
	}
	return a.forge.Comment(ctx, a.ref.Repo, a.ref.Number, body)
}

// dryRunRequested reads the flag without owning it. run and address define the
// real flag set; this only needs to know whether to record instead of post.
func dryRunRequested(argv []string) bool {
	if os.Getenv("SIMPLYCUBED_DRY_RUN") != "" {
		return true
	}
	for _, arg := range argv {
		switch arg {
		case "--dry-run", "-dry-run", "--dry-run=true", "-dry-run=true":
			return true
		}
	}
	return false
}

// reply answers on whatever thread the arguments point at.
func reply(argv []string, body string, stdout io.Writer) error {
	a, err := newAnswerer(argv)
	if err != nil {
		return err
	}
	return a.post(body, stdout)
}

// preflightCmd validates the repo config and engine settings, then exits. A
// workflow runs it before installing the rest of the toolchain, so a
// misconfigured repository finds out in seconds and the rules live in one place.
// checkMentionAgreement fails when the configured handle and the caller
// workflow's trigger disagree.
//
// The handle necessarily lives in two files: the parser reads it from config,
// which the App can push, while the trigger has to be a literal in the workflow,
// which the App deliberately cannot. Drift between them is silent in the worst
// way — comments simply stop working, with no error anywhere — so it is checked
// on every run rather than left to be discovered.
func checkMentionAgreement(repoDir, appName string) error {
	path := filepath.Join(repoDir, ".github", "workflows", "simplycubed.yml")
	b, err := os.ReadFile(path)
	if err != nil {
		// No caller workflow is a valid local setup, not a misconfiguration.
		return nil
	}
	want := "'" + command.MentionFor(appName) + "'"
	if strings.Contains(string(b), want) {
		return nil
	}
	return fmt.Errorf("%w: .github/simplycubed.yml sets appName %q, but %s does not trigger on %s. Comment commands will never fire. Re-run \"simplycubed init --workflow\" to rewrite the trigger from the config",
		ErrConfigMissing, appName, path, want)
}

func preflightCmd(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	actions := fs.Bool("actions", false, "also check the values only a caller workflow supplies")
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}
	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "simplycubed.yml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, err := engineEnv(cfg); err != nil {
		return err
	}
	if cfg.AppName == "" {
		return fmt.Errorf("%w: .github/simplycubed.yml sets no appName. Comment commands address the App you installed, so without it none of them can fire. Add \"appName: <your-app>\" or re-run \"simplycubed init --workflow --app-name <your-app>\"", ErrConfigMissing)
	}
	if err := checkMentionAgreement(*repoDir, cfg.AppName); err != nil {
		return err
	}
	// The App credentials are only reachable when the caller workflow exports
	// them. A local run authenticates as the operator and never uses them, so
	// checking them there would fail a working setup.
	if *actions {
		for _, v := range []struct {
			name  string
			where section
		}{
			{"SIMPLYCUBED_GH_APP_CLIENT_ID", sectionVariable},
			{"SIMPLYCUBED_GH_APP_PRIVATE_KEY", sectionSecret},
		} {
			if err := requireSet(v.name, v.where); err != nil {
				return err
			}
		}
	}
	fmt.Fprintln(stdout, "preflight ok: config and engine settings are present")
	return nil
}

// commonFlags are the flags shared by run and address, plus the resolved config
// and dependency graph both commands need.
type commonFlags struct {
	repoDir string
	base    string
	actor   string
	// dry is set only on a dry run, and holds the writes that were skipped.
	dry  *dryrun.Forge
	cfg  *config.Config
	deps app.Deps
}

// parseInterleaved parses argv with fs, allowing flags before or after
// positional arguments. Stdlib flag parsing stops at the first positional, so
// `run owner/repo#1 --repo-dir /x` would silently ignore the flag while the
// usage text advertises exactly that form. Positionals are collected in order
// and parsing resumes after each one, so both orders (and a mix) are honored.
func parseInterleaved(fs *flag.FlagSet, argv []string) ([]string, error) {
	var positionals []string
	for {
		if err := fs.Parse(argv); err != nil {
			return nil, err
		}
		argv = fs.Args()
		if len(argv) == 0 {
			return positionals, nil
		}
		positionals = append(positionals, argv[0])
		argv = argv[1:]
	}
}

// prepare parses the shared flags, loads the repo config, validates the engine
// environment, writes the codex config, and builds the dependency graph. It
// returns the remaining positional arguments so each command can parse its own
// reference (an issue for run, a pull request for address).
// isBotLogin reports whether a login belongs to a GitHub App installation.
// The Actions runtime always authenticates as an App, and that App deliberately
// holds no workflows permission, so any bot identity is restricted. An empty
// login means a local run under a human's own credential, which can push
// workflow files and must not be blocked.
func isBotLogin(login string) bool {
	return strings.HasSuffix(login, "[bot]")
}

func prepare(name string, argv []string) (*commonFlags, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	model := fs.String("model", "gpt-5.4", "engine model/deployment name")
	base := fs.String("base", "origin/HEAD", "worktree base ref")
	stateDir := fs.String("state-dir", filepath.Join(os.TempDir(), "simplycubed"), "state directory")
	actor := fs.String("actor", "", "login that triggered this run; checked for write access")
	dryRun := fs.Bool("dry-run", false, "run the whole loop but make no GitHub writes and no push")
	rest, err := parseInterleaved(fs, argv)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "simplycubed.yml"))
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	endpoint, err := engineEnv(cfg)
	if err != nil {
		return nil, nil, err
	}

	codexHome := filepath.Join(*stateDir, "codex-home")
	if cfg.Engine != "claude" {
		if _, err := codex.WriteConfig(codexHome, codex.ProviderConfig{
			Model:   *model,
			BaseURL: endpoint + "/openai/v1",
			EnvKey:  "SIMPLYCUBED_AZURE_OPENAI_API_KEY",
		}); err != nil {
			return nil, nil, fmt.Errorf("write codex config: %w", err)
		}
	}

	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}

	forge := &forgegh.Forge{StateLabels: app.StateLabels(prefix), Self: os.Getenv("SIMPLYCUBED_GH_APP_LOGIN")}
	dry := *dryRun || os.Getenv("SIMPLYCUBED_DRY_RUN") != ""
	// When the caller did not name the identity, ask the credential who it is.
	// The workflow used to do this with a gh graphql call and hand the answer
	// back in an environment variable; the product can just look.
	if forge.Self == "" {
		if login, err := forge.Whoami(context.Background()); err == nil {
			forge.Self = login
		}
	}

	vcs := newVCS(forge.Self)
	vcs.DryRun = dry
	var forgeForLoop forge2.Forge = forge
	var dryForge *dryrun.Forge
	if dry {
		dryForge = dryrun.New(forge)
		forgeForLoop = dryForge
	}

	deps := app.Deps{
		Runner: newRunner(cfg, codexHome),
		// Self, when set, filters the agent's own review feedback out of the fix
		// loop. Empty for local runs, where the operator is a human, not the bot.
		Forge:                  forgeForLoop,
		VCS:                    vcs,
		Worktrees:              &worktree.Manager{RepoDir: *repoDir, BaseDir: filepath.Join(*stateDir, "worktrees")},
		WorkflowRestrictedPush: isBotLogin(forge.Self),
		SelfLogin:              forge.Self,
	}
	return &commonFlags{repoDir: *repoDir, base: *base, actor: *actor, cfg: cfg, deps: deps, dry: dryForge}, rest, nil
}

// reportDryRun prints the writes a dry run skipped, and appends them to the
// GitHub Actions run summary when there is one, so the result is readable on
// the run page rather than buried in step logs.
func reportDryRun(c *commonFlags, w io.Writer) {
	if c == nil || c.dry == nil {
		return
	}
	report := c.dry.Report()
	fmt.Fprintln(w)
	fmt.Fprintln(w, report)
	if path := os.Getenv("GITHUB_STEP_SUMMARY"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		fmt.Fprintf(f, "## SimplyCubed Code dry run\n\n```\n%s\n```\n", report)
	}
}

func runCmd(argv []string) error {
	c, rest, err := prepare("run", argv)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("missing issue ref (want owner/repo#N)")
	}
	iss, err := app.ParseIssueRef(rest[0])
	if err != nil {
		return err
	}
	if err := app.Authorize(context.Background(), c.deps, iss.Repo, c.actor); err != nil {
		return err
	}
	if err := fetchIssue(&iss); err != nil {
		return fmt.Errorf("fetch issue: %w", err)
	}

	if c.dry != nil {
		fmt.Println("DRY RUN: no GitHub writes and no push will happen.")
	}
	fmt.Printf("running %s#%d against gate %q ...\n", iss.Repo, iss.Number, c.cfg.Gate)
	res, err := app.Run(context.Background(), c.deps, c.cfg, iss, c.base)
	defer reportDryRun(c, os.Stdout)
	if err != nil {
		return err
	}
	if res.Outcome == loop.OutcomePROpened {
		fmt.Printf("PR opened after %d round(s): %s\n", res.Rounds, res.PRURL)
	} else {
		fmt.Printf("blocked after %d round(s): %s\n", res.Rounds, res.Reason)
	}
	return nil
}

func addressCmd(argv []string) error {
	c, rest, err := prepare("address", argv)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("missing pull-request ref (want owner/repo#PR)")
	}
	ref, err := app.ParseIssueRef(rest[0])
	if err != nil {
		return err
	}
	if err := app.Authorize(context.Background(), c.deps, ref.Repo, c.actor); err != nil {
		return err
	}

	fmt.Printf("addressing feedback on %s#%d against gate %q ...\n", ref.Repo, ref.Number, c.cfg.Gate)
	res, err := app.AddressPR(context.Background(), c.deps, c.cfg, ref.Repo, ref.Number)
	defer reportDryRun(c, os.Stdout)
	if err != nil {
		return err
	}
	switch res.Outcome {
	case loop.OutcomeChangesPushed:
		fmt.Printf("changes pushed after %d round(s); re-requested review\n", res.Rounds)
	case loop.OutcomeNoFeedback:
		fmt.Println("no new review feedback to address; nothing to do")
	default:
		fmt.Printf("blocked after %d round(s): %s\n", res.Rounds, res.Reason)
	}
	return nil
}

func initCmd(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	writeWorkflow := fs.Bool("workflow", false, "also write the Actions caller workflow")
	appNameFlag := fs.String("app-name", "", "the GitHub App you installed, for example acme-code; comment commands address it")
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}

	appName, err := resolveAppName(*repoDir, *appNameFlag)
	if err != nil {
		return err
	}

	configPath := filepath.Join(*repoDir, ".github", "simplycubed.yml")
	wroteConfig, err := writeStarterConfig(configPath, appName)
	if err != nil {
		return err
	}
	workflowPath := filepath.Join(*repoDir, ".github", "workflows", "simplycubed.yml")
	selftestPath := filepath.Join(*repoDir, ".github", "workflows", "simplycubed-selftest.yml")
	wroteWorkflow := false
	wroteSelftest := false
	if *writeWorkflow {
		wroteWorkflow, err = writeStarterFile(workflowPath, renderCallerWorkflow(buildinfo.Version, appName))
		if err != nil {
			return err
		}
		// The self-test answers questions only a real runner can answer. It is
		// written alongside the caller so a bad install fails in a minute here
		// rather than silently during a real run, and the next steps tell the
		// operator to remove it once it has done its job.
		wroteSelftest, err = writeStarterFile(selftestPath, selftestWorkflowTemplate)
		if err != nil {
			return err
		}
	}

	labels := app.StateLabels(config.DefaultLabelPrefix)
	created, err := (&forgegh.Forge{Dir: *repoDir}).EnsureLabels(context.Background(), labels)
	if err != nil {
		return err
	}

	if wroteConfig {
		fmt.Fprintf(stdout, "wrote %s\n", configPath)
	} else {
		fmt.Fprintf(stdout, "left existing %s unchanged\n", configPath)
	}
	if *writeWorkflow {
		if wroteWorkflow {
			fmt.Fprintf(stdout, "wrote %s\n", workflowPath)
		} else {
			fmt.Fprintf(stdout, "left existing %s unchanged\n", workflowPath)
		}
		if wroteSelftest {
			fmt.Fprintf(stdout, "wrote %s\n", selftestPath)
		} else {
			fmt.Fprintf(stdout, "left existing %s unchanged\n", selftestPath)
		}
	}
	if len(created) == 0 {
		fmt.Fprintln(stdout, "labels already present: no changes")
	} else {
		fmt.Fprintf(stdout, "created labels: %s\n", strings.Join(created, ", "))
	}
	createURL, resolved := appCreateURL(*repoDir)
	fmt.Fprintln(stdout, "next steps:")
	fmt.Fprintln(stdout, "  1. Create your OWN GitHub App. It has to be yours: its private key is what mints")
	fmt.Fprintln(stdout, "     the tokens that act on your repository, so a shared key would let its holder act")
	fmt.Fprintln(stdout, "     on every other installation. This is what keeps the agent inside your GitHub.")
	fmt.Fprintln(stdout, "       "+createURL)
	if !resolved {
		fmt.Fprintln(stdout, "       (if this repository belongs to an organisation, use")
		fmt.Fprintln(stdout, "        https://github.com/organizations/<org>/settings/apps/new instead)")
	}
	fmt.Fprintln(stdout, "     Name it anything you like; App names are globally unique, so you cannot reuse ours.")
	fmt.Fprintln(stdout, "     Permissions, and nothing else:  Contents: Read and write")
	fmt.Fprintln(stdout, "                                     Issues: Read and write")
	fmt.Fprintln(stdout, "                                     Pull requests: Read and write")
	fmt.Fprintln(stdout, "     Uncheck Active under Webhook. Actions triggers this runtime; an enabled webhook")
	fmt.Fprintln(stdout, "     with nothing listening only generates failures.")
	fmt.Fprintln(stdout, "     Set Any account under Where can this GitHub App be installed, if the App belongs")
	fmt.Fprintln(stdout, "     to your personal account and the repository belongs to an organisation. An App")
	fmt.Fprintln(stdout, "     restricted to its owner cannot be installed anywhere else, and this is the step")
	fmt.Fprintln(stdout, "     most often missed.")
	fmt.Fprintln(stdout, "  2. Generate a private key on the App settings page and keep the download. GitHub")
	fmt.Fprintln(stdout, "     shows it once. Note the Client ID there too, the Iv23 string.")
	fmt.Fprintln(stdout, "  3. Install the App on this repository, from Install App on the same page.")
	fmt.Fprintln(stdout, "     Reference: https://docs.github.com/apps/creating-github-apps")
	fmt.Fprintln(stdout, "  - write the real gate in .github/simplycubed.yml")
	fmt.Fprintln(stdout, "  - verify that gate is green on your main branch")
	fmt.Fprintln(stdout, "  - add two repository VARIABLES, under Settings > Secrets and variables > Actions > Variables:")
	fmt.Fprintln(stdout, "      SIMPLYCUBED_GH_APP_CLIENT_ID       the App Client ID, the Iv23 string on the App settings page")
	fmt.Fprintln(stdout, "      SIMPLYCUBED_AZURE_OPENAI_ENDPOINT  e.g. https://<resource>.openai.azure.com")
	fmt.Fprintln(stdout, "  - add two repository SECRETS, on the Secrets tab of that same page:")
	fmt.Fprintln(stdout, "      SIMPLYCUBED_GH_APP_PRIVATE_KEY     the full PEM, including the BEGIN and END lines")
	fmt.Fprintln(stdout, "      SIMPLYCUBED_AZURE_OPENAI_API_KEY   the Azure OpenAI key")
	fmt.Fprintln(stdout, "    Variables and Secrets are different tabs. A value filed under the wrong one reads back")
	fmt.Fprintln(stdout, "    as empty, and the run fails without saying why.")
	fmt.Fprintln(stdout, "  - merge the PR containing the config and workflow changes")
	fmt.Fprintln(stdout, "  - run the self-test once: gh workflow run simplycubed-selftest")
	fmt.Fprintln(stdout, "  - delete .github/workflows/simplycubed-selftest.yml once it passes")
	fmt.Fprintln(stdout, "  - file an issue and apply the sc:go label")
	return nil
}

// appCreateURL returns the GitHub form for creating an App, which differs by
// account type. Best effort: init must still work when gh cannot answer, so a
// failure returns the personal form and a note rather than an error.
func appCreateURL(repoDir string) (string, bool) {
	out, err := exec.Command("gh", "repo", "view", "--json", "owner", "--jq", ".owner.login").Output()
	_ = repoDir
	if err != nil {
		return "https://github.com/settings/apps/new", false
	}
	owner := strings.TrimSpace(string(out))
	if owner == "" {
		return "https://github.com/settings/apps/new", false
	}
	kind, err := exec.Command("gh", "api", "users/"+owner, "--jq", ".type").Output()
	if err != nil {
		return "https://github.com/settings/apps/new", false
	}
	if strings.TrimSpace(string(kind)) == "Organization" {
		return "https://github.com/organizations/" + owner + "/settings/apps/new", true
	}
	return "https://github.com/settings/apps/new", true
}

func writeStarterConfig(path, appName string) (bool, error) {
	return writeStarterFile(path, strings.ReplaceAll(starterConfig, callerWorkflowAppNameToken, appName))
}

// resolveAppName decides the handle to write into both files. An explicit
// --app-name wins; otherwise an existing config keeps what it already says, so
// re-running init to pick up a new release does not silently change the handle
// a team already types.
func resolveAppName(repoDir, flagValue string) (string, error) {
	if v := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(flagValue), "@"), "[bot]"); v != "" {
		return v, nil
	}
	if cfg, err := config.Load(filepath.Join(repoDir, ".github", "simplycubed.yml")); err == nil && cfg.AppName != "" {
		return cfg.AppName, nil
	}
	return "", errors.New("--app-name is required: comment commands address the App you installed, and its name is unique to you. Pass the App's name without the \"[bot]\" suffix, for example --app-name acme-code")
}

func writeStarterFile(path, body string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// renderCallerWorkflow writes the trigger for the App this repository installed.
// The handle cannot be a constant: App names are globally unique, so every
// adopter's bot has its own, and the trigger has to match theirs or no comment
// ever starts a run. It is templated here rather than matched loosely at
// runtime so a mention of a colleague does not spin up a runner.
func renderCallerWorkflow(version, appName string) string {
	out := strings.ReplaceAll(callerWorkflowTemplate, callerWorkflowTagToken, workflowTemplateTag(version))
	return strings.ReplaceAll(out, callerWorkflowAppNameToken, appName)
}

func workflowTemplateTag(version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	if isReleaseVersion(version) {
		return "v" + version
	}
	return latestKnownWorkflowTag
}

func isReleaseVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// fetchIssue fills the issue title and body from GitHub via gh.
func fetchIssue(iss *domain.Issue) error {
	out, err := exec.Command("gh", "issue", "view", strconv.Itoa(iss.Number),
		"--repo", iss.Repo, "--json", "title,body").Output()
	if err != nil {
		return err
	}
	var v struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		return err
	}
	iss.Title, iss.Body = v.Title, v.Body
	return nil
}
