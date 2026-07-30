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

run flags:
  --repo-dir    path to the target repo checkout (default ".")
  --model       engine model/deployment name (default "gpt-5.4")
  --base        worktree base ref (default "origin/HEAD")
  --state-dir   where worktrees and the generated config live

environment (engine settings; never committed to a repo):
  AZURE_OPENAI_ENDPOINT   e.g. https://<resource>.openai.azure.com
  AZURE_OPENAI_API_KEY    the key, read by name and never written to disk
`, buildinfo.Version)
}

func runCmd(argv []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	repoDir := fs.String("repo-dir", ".", "path to the target repo checkout")
	model := fs.String("model", "gpt-5.4", "engine model/deployment name")
	base := fs.String("base", "origin/HEAD", "worktree base ref")
	stateDir := fs.String("state-dir", filepath.Join(os.TempDir(), "simplycubed"), "state directory")
	if err := fs.Parse(argv); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("missing issue ref (want owner/repo#N)")
	}
	iss, err := app.ParseIssueRef(fs.Arg(0))
	if err != nil {
		return err
	}

	cfg, err := config.Load(filepath.Join(*repoDir, ".github", "simplycubed.yml"))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	endpoint := strings.TrimRight(os.Getenv("AZURE_OPENAI_ENDPOINT"), "/")
	if endpoint == "" {
		return fmt.Errorf("AZURE_OPENAI_ENDPOINT is not set")
	}
	if os.Getenv("AZURE_OPENAI_API_KEY") == "" {
		return fmt.Errorf("AZURE_OPENAI_API_KEY is not set")
	}

	codexHome := filepath.Join(*stateDir, "codex-home")
	if _, err := codex.WriteConfig(codexHome, codex.ProviderConfig{
		Model:   *model,
		BaseURL: endpoint + "/openai/v1",
		EnvKey:  "AZURE_OPENAI_API_KEY",
	}); err != nil {
		return fmt.Errorf("write codex config: %w", err)
	}

	if err := fetchIssue(&iss); err != nil {
		return fmt.Errorf("fetch issue: %w", err)
	}

	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}

	deps := app.Deps{
		Runner:    codex.New(codexHome),
		Forge:     &forgegh.Forge{StateLabels: app.StateLabels(prefix)},
		VCS:       &vcsgit.Git{},
		Worktrees: &worktree.Manager{RepoDir: *repoDir, BaseDir: filepath.Join(*stateDir, "worktrees")},
	}

	fmt.Printf("running %s#%d against gate %q ...\n", iss.Repo, iss.Number, cfg.Gate)
	res, err := app.Run(context.Background(), deps, cfg, iss, *base)
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
