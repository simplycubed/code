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
  AZURE_OPENAI_ENDPOINT   e.g. https://<resource>.openai.azure.com
  AZURE_OPENAI_API_KEY    the key, read by name and never written to disk
`, buildinfo.Version)
}

const starterConfig = `# Repository-local contract for SimplyCubed Code.
# Keep the label prefix unless you already run another sc:* lifecycle.
labelPrefix: sc

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
	latestKnownWorkflowTag = "v0.1.8"
	callerWorkflowTagToken = "__SIMPLYCUBED_TAG__"
	simplycubedAppLogin    = "simplycubed-code[bot]"
)

//go:embed simplycubed-caller.yml.tmpl
var callerWorkflowTemplate string

//go:embed simplycubed-selftest.yml.tmpl
var selftestWorkflowTemplate string

// engineEnv validates the engine settings and returns the normalized endpoint.
// It is the single implementation: `prepare` calls it before a run, and the
// `preflight` command calls it so a workflow can fail early without a second
// copy of these rules written in shell.
func engineEnv() (string, error) {
	endpoint := strings.TrimRight(os.Getenv("AZURE_OPENAI_ENDPOINT"), "/")
	if endpoint == "" {
		return "", fmt.Errorf("AZURE_OPENAI_ENDPOINT is not set. It is a repository variable on your own repository; a reusable workflow never inherits variables from SimplyCubed")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("AZURE_OPENAI_ENDPOINT is not a valid URL: %w", err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return "", fmt.Errorf("AZURE_OPENAI_ENDPOINT must be an https URL like https://<resource>.openai.azure.com, got %q", endpoint)
	}
	if os.Getenv("AZURE_OPENAI_API_KEY") == "" {
		return "", fmt.Errorf("AZURE_OPENAI_API_KEY is not set. It is a repository secret on your own repository; a reusable workflow never inherits secrets from SimplyCubed")
	}
	return endpoint, nil
}

// newVCS builds the git layer with a committer identity. A GitHub Actions
// runner has none configured, so without this a commit fails with "Author
// identity unknown" after the change is already made and the gate has passed.
// The identity is the credential's own login, so commits are attributable to
// whoever the run authenticated as.
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
// Note that the default sandbox uses bubblewrap on Linux, which cannot create a
// network namespace inside a stock GitHub Actions runner:
//
//	bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted
//
// Every command the engine tries then fails, including `pwd`, so it reads
// nothing and changes nothing while still exiting successfully. The answer to
// that is not to widen the sandbox: it is that the run stops and a human
// finishes the work. See docs/faq.md, and the self-test, which reports this
// condition in one minute instead of after a run that appeared to succeed.
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
	kind := command.Parse(body)

	switch kind {
	case command.Help:
		return reply(rest, command.HelpText, stdout)
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
			return a.post(command.MisdirectedText(kind, a.onPR), stdout)
		}
		if kind == command.Go {
			return runCmd(rest)
		}
		return addressCmd(rest)
	default:
		// Only answer a comment that was actually addressed to the agent.
		// Anything else is someone mentioning it in passing, and replying to
		// that would make it the noisiest participant in every thread.
		if command.Addressed(body) {
			return reply(rest, command.UnknownText, stdout)
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
func preflightCmd(argv []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("preflight", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}
	if _, err := config.Load(filepath.Join(*repoDir, ".github", "simplycubed.yml")); err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if _, err := engineEnv(); err != nil {
		return err
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

	endpoint, err := engineEnv()
	if err != nil {
		return nil, nil, err
	}

	codexHome := filepath.Join(*stateDir, "codex-home")
	if _, err := codex.WriteConfig(codexHome, codex.ProviderConfig{
		Model:   *model,
		BaseURL: endpoint + "/openai/v1",
		EnvKey:  "AZURE_OPENAI_API_KEY",
	}); err != nil {
		return nil, nil, fmt.Errorf("write codex config: %w", err)
	}

	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}

	forge := &forgegh.Forge{StateLabels: app.StateLabels(prefix), Self: os.Getenv("SIMPLYCUBED_SELF_LOGIN")}
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
		WorkflowRestrictedPush: forge.Self == simplycubedAppLogin,
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
	if _, err := parseInterleaved(fs, argv); err != nil {
		return err
	}

	configPath := filepath.Join(*repoDir, ".github", "simplycubed.yml")
	wroteConfig, err := writeStarterConfig(configPath)
	if err != nil {
		return err
	}
	workflowPath := filepath.Join(*repoDir, ".github", "workflows", "simplycubed.yml")
	selftestPath := filepath.Join(*repoDir, ".github", "workflows", "simplycubed-selftest.yml")
	wroteWorkflow := false
	wroteSelftest := false
	if *writeWorkflow {
		wroteWorkflow, err = writeStarterFile(workflowPath, renderCallerWorkflow(buildinfo.Version))
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
	fmt.Fprintln(stdout, "next steps:")
	fmt.Fprintln(stdout, "  - install the simplycubed-code GitHub App with contents, issues, and pull requests permissions only")
	fmt.Fprintln(stdout, "  - disable the App webhook, set install visibility to Any account, and install it on the repo")
	fmt.Fprintln(stdout, "  - write the real gate in .github/simplycubed.yml")
	fmt.Fprintln(stdout, "  - verify that gate is green on your main branch")
	fmt.Fprintln(stdout, "  - set the SIMPLYCUBED_GH_APP_CLIENT_ID repo variable to the App Client ID, the Iv23 string on the App settings page")
	fmt.Fprintln(stdout, "  - add the SIMPLYCUBED_GH_APP_PRIVATE_KEY repo secret with the full PEM, including BEGIN/END lines")
	fmt.Fprintln(stdout, "  - set the AZURE_OPENAI_ENDPOINT repo variable")
	fmt.Fprintln(stdout, "  - add the AZURE_OPENAI_API_KEY repo secret")
	fmt.Fprintln(stdout, "  - merge the PR containing the config and workflow changes")
	fmt.Fprintln(stdout, "  - run the self-test once: gh workflow run simplycubed-selftest")
	fmt.Fprintln(stdout, "  - delete .github/workflows/simplycubed-selftest.yml once it passes")
	fmt.Fprintln(stdout, "  - file an issue and apply the sc:go label")
	return nil
}

func writeStarterConfig(path string) (bool, error) {
	return writeStarterFile(path, starterConfig)
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

func renderCallerWorkflow(version string) string {
	return strings.ReplaceAll(callerWorkflowTemplate, callerWorkflowTagToken, workflowTemplateTag(version))
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
