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
	"encoding/json"
	"flag"
	"fmt"
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
