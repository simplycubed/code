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

// Commit stages all changes and commits them. It returns committed=false, with
// no error, when the working tree has nothing to commit.
func (g *Git) Commit(ctx context.Context, dir, message string) (bool, error) {
	if _, err := g.run(ctx, dir, "add", "-A"); err != nil {
		return false, err
	}
	status, err := g.run(ctx, dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	if strings.TrimSpace(status) == "" {
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
