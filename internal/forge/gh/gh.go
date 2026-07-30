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
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/simplycubed/code/internal/domain"
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
	// Self is the agent's own GitHub login (for example "simplycubed-code[bot]").
	// When set, review feedback authored by it is excluded from Feedback, so the
	// fixer never treats the agent's own output as work to do. Empty disables the
	// author filter (used for local runs); freshness still bounds the result.
	Self string
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

// runJSON runs gh capturing stdout only, so a notice gh prints to stderr (a
// deprecation, an auth or rate-limit warning) never contaminates the JSON that
// stdout is expected to carry. On failure it surfaces stderr in the error.
func (f *Forge) runJSON(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, f.bin(), args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
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

// CommentPR posts a comment on a pull request.
func (f *Forge) CommentPR(ctx context.Context, repo string, pr int, body string) error {
	_, err := f.run(ctx, "pr", "comment", strconv.Itoa(pr), "--repo", repo, "--body", body)
	return err
}

// prMeta is the pull-request head and title, read once so feedback can be
// filtered to the current head commit.
type prMeta struct {
	HeadRefName string `json:"headRefName"`
	HeadRefOid  string `json:"headRefOid"`
	Title       string `json:"title"`
}

type ghReview struct {
	User     struct{ Login string } `json:"user"`
	Body     string                 `json:"body"`
	State    string                 `json:"state"`
	CommitID string                 `json:"commit_id"`
}

type ghReviewComment struct {
	User     struct{ Login string } `json:"user"`
	Body     string                 `json:"body"`
	Path     string                 `json:"path"`
	Line     int                    `json:"line"`
	OrigLine int                    `json:"original_line"`
	CommitID string                 `json:"commit_id"`
}

// paginate fetches every page of a gh api array endpoint as one flat slice. It
// uses `--paginate --slurp`: plain `--paginate` concatenates the per-page arrays
// into invalid JSON (`[...][...]`), while `--slurp` returns a single array whose
// elements are the per-page arrays, which are flattened here. Without this, any
// pull request with more than one page of reviews or comments would fail to
// parse.
func paginate[T any](ctx context.Context, f *Forge, endpoint string) ([]T, error) {
	out, err := f.runJSON(ctx, "api", endpoint, "--paginate", "--slurp")
	if err != nil {
		return nil, err
	}
	var pages [][]T
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	var all []T
	for _, p := range pages {
		all = append(all, p...)
	}
	return all, nil
}

// Feedback gathers the actionable human review on a pull request. "Actionable"
// means left against the current head commit (so already-addressed feedback on an
// earlier commit falls away once the fixer pushes) and not authored by the agent
// itself. Two GitHub surfaces are read: review summaries (the /reviews endpoint)
// and inline comments (the /comments endpoint).
func (f *Forge) Feedback(ctx context.Context, repo string, pr int) (domain.ReviewFeedback, error) {
	metaOut, err := f.runJSON(ctx, "pr", "view", strconv.Itoa(pr), "--repo", repo,
		"--json", "headRefName,headRefOid,title")
	if err != nil {
		return domain.ReviewFeedback{}, err
	}
	var meta prMeta
	if err := json.Unmarshal(metaOut, &meta); err != nil {
		return domain.ReviewFeedback{}, fmt.Errorf("gh pr view: parse: %w", err)
	}
	fb := domain.ReviewFeedback{PR: pr, Branch: meta.HeadRefName, HeadSHA: meta.HeadRefOid, Title: meta.Title}

	reviews, err := paginate[ghReview](ctx, f, fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, pr))
	if err != nil {
		return domain.ReviewFeedback{}, fmt.Errorf("gh api reviews: %w", err)
	}
	for _, r := range reviews {
		if !f.actionable(r.User.Login, r.CommitID, meta.HeadRefOid) {
			continue
		}
		if r.State != "CHANGES_REQUESTED" && r.State != "COMMENTED" {
			continue
		}
		if strings.TrimSpace(r.Body) == "" {
			continue // a review whose content is only inline comments; captured below
		}
		fb.Notes = append(fb.Notes, domain.ReviewNote{Author: r.User.Login, Body: strings.TrimSpace(r.Body)})
	}

	comments, err := paginate[ghReviewComment](ctx, f, fmt.Sprintf("repos/%s/pulls/%d/comments", repo, pr))
	if err != nil {
		return domain.ReviewFeedback{}, fmt.Errorf("gh api comments: %w", err)
	}
	for _, c := range comments {
		if !f.actionable(c.User.Login, c.CommitID, meta.HeadRefOid) {
			continue
		}
		line := c.Line
		if line == 0 {
			line = c.OrigLine
		}
		fb.Notes = append(fb.Notes, domain.ReviewNote{
			Author: c.User.Login, File: c.Path, Line: line, Body: strings.TrimSpace(c.Body),
		})
	}
	return fb, nil
}

// actionable reports whether a piece of feedback should be addressed: it must be
// left against the current head commit and not authored by the agent itself.
func (f *Forge) actionable(author, commitID, headSHA string) bool {
	if commitID != headSHA {
		return false
	}
	if f.Self != "" && author == f.Self {
		return false
	}
	return true
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
