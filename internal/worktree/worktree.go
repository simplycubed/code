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
//
// base is resolved before use: see ResolveBase for why the default cannot be
// taken literally in every checkout.
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
	base, err = m.ResolveBase(ctx, base)
	if err != nil {
		return "", err
	}
	path := filepath.Join(m.BaseDir, sanitize(branch))
	if out, err := m.git(ctx, "worktree", "add", "-B", branch, path, base); err != nil {
		return "", fmt.Errorf("worktree add %s: %w: %s", branch, err, strings.TrimSpace(out))
	}
	return path, nil
}

// ResolveBase returns a ref that actually exists, given the requested base.
//
// A normal clone has origin/HEAD, so the "origin/HEAD" default resolves and this
// is a no-op. A GitHub Actions checkout does not: actions/checkout never creates
// that symbolic ref, so the default base is an invalid reference and every run
// fails at worktree creation. Rather than push the workaround onto every caller,
// resolve it here: ask the remote for its default branch, and fall back to the
// conventional names before giving up.
func (m *Manager) ResolveBase(ctx context.Context, base string) (string, error) {
	if m.refExists(ctx, base) {
		return base, nil
	}
	if base != "origin/HEAD" {
		return "", fmt.Errorf("base ref %q does not exist in %s", base, m.RepoDir)
	}
	// Ask the remote which branch it points HEAD at, then re-check.
	if _, err := m.git(ctx, "remote", "set-head", "origin", "--auto"); err == nil {
		if m.refExists(ctx, base) {
			return base, nil
		}
	}
	for _, candidate := range []string{"origin/main", "origin/master"} {
		if m.refExists(ctx, candidate) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("cannot resolve a base ref: %q does not exist and no default branch was found in %s", base, m.RepoDir)
}

func (m *Manager) refExists(ctx context.Context, ref string) bool {
	_, err := m.git(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	return err == nil
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
