// Package gh implements forge.Forge using the GitHub CLI (`gh`). It inherits the
// gh process's own authentication; this package holds no token of its own.
//
// Interface decision, stated deliberately: OpenPR does NOT push. The head branch
// must already exist on the remote before OpenPR is called. This type performs
// GitHub operations (`gh pr/issue ...`), not git operations; pushing the branch
// is the caller's job (the worktree/loop layer), which keeps git and GitHub
// concerns separate.
package gh

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Forge drives GitHub via the gh CLI.
type Forge struct {
	// Bin is the gh binary; defaults to "gh".
	Bin string
	// StateLabels is the full set of mutually-exclusive state labels (for example
	// "sc:go", "sc:queued", ... "sc:done"). SetState removes every one of these
	// other than the label being set, so exactly one state label remains on the
	// issue. GitHub does not enforce that; this does.
	StateLabels []string
}

func (f *Forge) bin() string {
	if f.Bin != "" {
		return f.Bin
	}
	return "gh"
}

func (f *Forge) run(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, f.bin(), args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// OpenPR creates a pull request from an already-pushed head branch and returns
// its URL. See the package doc: the branch must exist on the remote first.
func (f *Forge) OpenPR(ctx context.Context, repo, branch, title, body string) (string, error) {
	out, err := f.run(ctx, "pr", "create", "--repo", repo, "--head", branch, "--title", title, "--body", body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(lastNonEmptyLine(out)), nil
}

// SetState applies label to the issue and removes every other state label, so
// exactly one remains.
func (f *Forge) SetState(ctx context.Context, repo string, issue int, label string) error {
	args := []string{"issue", "edit", strconv.Itoa(issue), "--repo", repo, "--add-label", label}
	for _, l := range f.StateLabels {
		if l != label {
			args = append(args, "--remove-label", l)
		}
	}
	_, err := f.run(ctx, args...)
	return err
}

// Comment posts a comment on the issue.
func (f *Forge) Comment(ctx context.Context, repo string, issue int, body string) error {
	_, err := f.run(ctx, "issue", "comment", strconv.Itoa(issue), "--repo", repo, "--body", body)
	return err
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i]
		}
	}
	return ""
}
