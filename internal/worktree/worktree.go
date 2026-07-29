// Package worktree manages an isolated git worktree per issue. Each active loop
// works in its own worktree, so concurrent runs never share a checkout and never
// step on each other's files. Creating and removing worktrees is done by shelling
// out to git; there is no git library dependency.
package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Manager creates and removes worktrees under BaseDir for the checkout at
// RepoDir.
type Manager struct {
	RepoDir string // the main git checkout
	BaseDir string // where per-issue worktrees are created
}

type entry struct {
	Path   string
	Branch string
}

// Add creates a worktree for branch, starting from base (for example
// "origin/main" or "HEAD"), and returns its path. If a worktree for that branch
// already exists it is reused and returned, which makes a re-run idempotent (git
// refuses to check the same branch out in two worktrees, so reuse is the correct
// behavior rather than an error).
func (m *Manager) Add(ctx context.Context, branch, base string) (string, error) {
	existing, err := m.entries(ctx)
	if err != nil {
		return "", err
	}
	for _, e := range existing {
		if e.Branch == branch {
			return e.Path, nil
		}
	}
	path := filepath.Join(m.BaseDir, sanitize(branch))
	if out, err := m.git(ctx, "worktree", "add", "-B", branch, path, base); err != nil {
		return "", fmt.Errorf("worktree add %s: %w: %s", branch, err, strings.TrimSpace(out))
	}
	return path, nil
}

// Remove deletes a worktree.
func (m *Manager) Remove(ctx context.Context, path string) error {
	if out, err := m.git(ctx, "worktree", "remove", "--force", path); err != nil {
		return fmt.Errorf("worktree remove %s: %w: %s", path, err, strings.TrimSpace(out))
	}
	return nil
}

// List returns the paths of the worktrees git knows about, including the main
// checkout.
func (m *Manager) List(ctx context.Context) ([]string, error) {
	es, err := m.entries(ctx)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(es))
	for _, e := range es {
		paths = append(paths, e.Path)
	}
	return paths, nil
}

// entries parses `git worktree list --porcelain` into path/branch pairs.
func (m *Manager) entries(ctx context.Context) ([]entry, error) {
	out, err := m.git(ctx, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("worktree list: %w: %s", err, strings.TrimSpace(out))
	}
	var es []entry
	var cur entry
	for _, line := range strings.Split(out, "\n") {
		if p, ok := strings.CutPrefix(line, "worktree "); ok {
			if cur.Path != "" {
				es = append(es, cur)
			}
			cur = entry{Path: p}
		} else if b, ok := strings.CutPrefix(line, "branch "); ok {
			cur.Branch = strings.TrimPrefix(b, "refs/heads/")
		}
	}
	if cur.Path != "" {
		es = append(es, cur)
	}
	return es, nil
}

func (m *Manager) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = m.RepoDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func sanitize(branch string) string {
	return strings.ReplaceAll(branch, "/", "-")
}
