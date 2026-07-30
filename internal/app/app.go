// Package app composes the pieces into a runnable pipeline: it onboards an
// isolated worktree for an issue and runs the loop against the repo's own gate,
// with the role prompts (and their bounds) wired in. The concrete engine, forge,
// and VCS are injected, so the pipeline is testable without a model or network.
package app

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine"
	"github.com/simplycubed/code/internal/forge"
	"github.com/simplycubed/code/internal/gate"
	"github.com/simplycubed/code/internal/loop"
	"github.com/simplycubed/code/internal/roles"
	"github.com/simplycubed/code/internal/state"
	"github.com/simplycubed/code/internal/worktree"
)

// Deps are the injected collaborators.
type Deps struct {
	Runner    engine.Runner
	Forge     forge.Forge
	VCS       loop.VCS
	Worktrees *worktree.Manager
}

var issueRefRE = regexp.MustCompile(`^([^/\s]+/[^/#\s]+)#(\d+)$`)

// ParseIssueRef parses an "owner/repo#123" reference.
func ParseIssueRef(s string) (domain.Issue, error) {
	m := issueRefRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return domain.Issue{}, fmt.Errorf("app: cannot parse issue ref %q (want owner/repo#N)", s)
	}
	n, _ := strconv.Atoi(m[2])
	return domain.Issue{Repo: m[1], Number: n}, nil
}

// StateLabels returns the full set of state labels for a prefix, for configuring
// the forge's one-state-label enforcement.
func StateLabels(prefix string) []string {
	all := state.All()
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, state.Label(prefix, s))
	}
	return out
}

// agentScratch are worktree-local paths the agent may write but that must never
// be committed (the workspace-local Go build cache the codex adapter sets, and a
// reserved dir for future scratch).
var agentScratch = []string{".gocache/", ".simplycubed/"}

// excludeAgentScratch appends the agent-scratch patterns to the worktree's local
// git exclude, so they are ignored by `git add -A` without touching the target
// repo's committed .gitignore. Best-effort: a failure here is not fatal.
func excludeAgentScratch(ctx context.Context, worktree string) {
	out, err := exec.CommandContext(ctx, "git", "-C", worktree, "rev-parse", "--git-path", "info/exclude").Output()
	if err != nil {
		return
	}
	path := strings.TrimSpace(string(out))
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n# SimplyCubed Code agent scratch (not committed)\n" + strings.Join(agentScratch, "\n") + "\n")
}

func promptBuilder(gateCmd string) func(domain.Role, domain.Issue) string {
	return func(role domain.Role, iss domain.Issue) string {
		def := roles.Implementer
		if role == domain.RoleReviewer {
			def = roles.Reviewer
		}
		return roles.Assemble(def, iss, gateCmd)
	}
}

// Run onboards a worktree for the issue and runs the loop against cfg.Gate. base
// is the ref the worktree branches from (for example "origin/main").
func Run(ctx context.Context, d Deps, cfg *config.Config, iss domain.Issue, base string) (loop.Result, error) {
	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}
	branch := fmt.Sprintf("%s/%d", prefix, iss.Number)

	wt, err := d.Worktrees.Add(ctx, branch, base)
	if err != nil {
		return loop.Result{}, fmt.Errorf("app: prepare worktree: %w", err)
	}
	// Keep agent scratch (build caches, etc.) out of the commit. It lives inside
	// the worktree so the sandbox can write it, so it must be excluded here or a
	// `git add -A` would sweep it into the PR (and mask a no-op run as real work).
	excludeAgentScratch(ctx, wt)

	eng := &loop.Engine{
		Runner: d.Runner,
		Gate:   func(ctx context.Context, dir string) gate.Result { return gate.Run(ctx, dir, cfg.Gate) },
		Forge:  d.Forge,
		VCS:    d.VCS,
		Prompt: promptBuilder(cfg.Gate),
		Cfg:    loop.Config{WorkDir: wt, Branch: branch, LabelPrefix: prefix},
	}
	return eng.Run(ctx, iss)
}
