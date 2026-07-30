// Package git implements loop.VCS by shelling out to git. It stages and commits
// the agent's changes and pushes the branch so a pull request can be opened
// against it.
package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Git runs git commands in a working directory.
type Git struct {
	// Bin is the git binary; defaults to "git".
	Bin string
	// Remote is the push remote; defaults to "origin".
	Remote string
	// ExcludePaths are worktree-relative paths that must never be staged (agent
	// scratch such as a build cache the engine writes inside the worktree).
	// Defaults to .gocache and .simplycubed.
	ExcludePaths []string
}

func (g *Git) excludePathspecs() []string {
	ps := g.ExcludePaths
	if ps == nil {
		ps = []string{".gocache", ".simplycubed"}
	}
	out := make([]string, 0, len(ps)*2)
	for _, p := range ps {
		p = strings.TrimSuffix(p, "/")
		out = append(out, ":(exclude)"+p, ":(exclude)"+p+"/**")
	}
	return out
}

func (g *Git) bin() string {
	if g.Bin != "" {
		return g.Bin
	}
	return "git"
}

func (g *Git) remote() string {
	if g.Remote != "" {
		return g.Remote
	}
	return "origin"
}

func (g *Git) run(ctx context.Context, dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, g.bin(), full...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Commit stages all changes except the excluded scratch paths and commits them.
// It returns committed=false, with no error, when nothing (outside the excluded
// paths) is staged. Excluding at add time means agent scratch such as a build
// cache never lands in the commit, and an otherwise no-op run is correctly
// reported as "nothing to commit" even if scratch files exist.
func (g *Git) Commit(ctx context.Context, dir, message string) (bool, error) {
	addArgs := append([]string{"add", "-A", "--", "."}, g.excludePathspecs()...)
	if _, err := g.run(ctx, dir, addArgs...); err != nil {
		return false, err
	}
	// `diff --cached --quiet` exits 0 when nothing is staged. This looks only at
	// the index, so untracked (excluded) scratch does not count as a change.
	staged := exec.CommandContext(ctx, g.bin(), "-C", dir, "diff", "--cached", "--quiet")
	if staged.Run() == nil {
		return false, nil
	}
	if _, err := g.run(ctx, dir, "commit", "-m", message); err != nil {
		return false, err
	}
	return true, nil
}

// Push pushes branch from dir to the remote.
func (g *Git) Push(ctx context.Context, dir, branch string) error {
	_, err := g.run(ctx, dir, "push", g.remote(), branch)
	return err
}
