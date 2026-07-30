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
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/simplycubed/code/internal/app"
	"github.com/simplycubed/code/internal/buildinfo"
	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine/codex"
	forgegh "github.com/simplycubed/code/internal/forge/gh"
	"github.com/simplycubed/code/internal/loop"
	vcsgit "github.com/simplycubed/code/internal/vcs/git"
	"github.com/simplycubed/code/internal/worktree"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(buildinfo.Version)
	case "init":
		if err := initCmd(args[1:], os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "run":
		if err := runCmd(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "address":
		if err := addressCmd(args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `simplycubed %s

usage:
  simplycubed version
  simplycubed init [--repo-dir .] [--workflow]
  simplycubed run <owner/repo#N> [flags]
  simplycubed address <owner/repo#PR> [flags]

run drives an issue to a pull request. address runs the fix-on-request loop
over an open pull request: it reads the human's review feedback and pushes a
fix back to the same branch, or reports that there is nothing new to address.

flags (both commands):
  --repo-dir    path to the target repo checkout (default ".")
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
	latestKnownWorkflowTag = "v0.1.3"
	callerWorkflowTagToken = "__SIMPLYCUBED_TAG__"
)

//go:embed simplycubed-caller.yml.tmpl
var callerWorkflowTemplate string

// commonFlags are the flags shared by run and address, plus the resolved config
// and dependency graph both commands need.
type commonFlags struct {
	repoDir string
	base    string
	cfg     *config.Config
	deps    app.Deps
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
	rest, err := parseInterleaved(fs, argv)
	if err != nil {
		return nil, nil, err
	}

	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "simplycubed.yml"))
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}

	endpoint := strings.TrimRight(os.Getenv("AZURE_OPENAI_ENDPOINT"), "/")
	if endpoint == "" {
		return nil, nil, fmt.Errorf("AZURE_OPENAI_ENDPOINT is not set")
	}
	if os.Getenv("AZURE_OPENAI_API_KEY") == "" {
		return nil, nil, fmt.Errorf("AZURE_OPENAI_API_KEY is not set")
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

	deps := app.Deps{
		Runner: codex.New(codexHome),
		// Self, when set, filters the agent's own review feedback out of the fix
		// loop. Empty for local runs, where the operator is a human, not the bot.
		Forge:     &forgegh.Forge{StateLabels: app.StateLabels(prefix), Self: os.Getenv("SIMPLYCUBED_SELF_LOGIN")},
		VCS:       &vcsgit.Git{},
		Worktrees: &worktree.Manager{RepoDir: *repoDir, BaseDir: filepath.Join(*stateDir, "worktrees")},
	}
	return &commonFlags{repoDir: *repoDir, base: *base, cfg: cfg, deps: deps}, rest, nil
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
	if err := fetchIssue(&iss); err != nil {
		return fmt.Errorf("fetch issue: %w", err)
	}

	fmt.Printf("running %s#%d against gate %q ...\n", iss.Repo, iss.Number, c.cfg.Gate)
	res, err := app.Run(context.Background(), c.deps, c.cfg, iss, c.base)
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

	fmt.Printf("addressing feedback on %s#%d against gate %q ...\n", ref.Repo, ref.Number, c.cfg.Gate)
	res, err := app.AddressPR(context.Background(), c.deps, c.cfg, ref.Repo, ref.Number)
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
	wroteWorkflow := false
	if *writeWorkflow {
		wroteWorkflow, err = writeStarterFile(workflowPath, renderCallerWorkflow(buildinfo.Version))
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
	fmt.Fprintln(stdout, "  - set the SIMPLYCUBED_GH_APP_ID repo variable")
	fmt.Fprintln(stdout, "  - add the SIMPLYCUBED_GH_APP_PRIVATE_KEY repo secret with the full PEM, including BEGIN/END lines")
	fmt.Fprintln(stdout, "  - set the AZURE_OPENAI_ENDPOINT repo variable")
	fmt.Fprintln(stdout, "  - add the AZURE_OPENAI_API_KEY repo secret")
	fmt.Fprintln(stdout, "  - merge the PR containing the config and workflow changes")
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
