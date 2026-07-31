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
	// Dir is the working directory for gh commands. Empty uses the current
	// process directory.
	Dir string
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

func (f *Forge) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, f.bin(), args...)
	if f.Dir != "" {
		cmd.Dir = f.Dir
	}
	return cmd
}

func (f *Forge) run(ctx context.Context, args ...string) (string, error) {
	cmd := f.command(ctx, args...)
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
	cmd := f.command(ctx, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return []byte(stdout.String()), nil
}

type ghLabel struct {
	Name string `json:"name"`
}

// EnsureLabels creates each label that does not already exist in the repo and
// returns the labels it created. Existing labels are left untouched.
func (f *Forge) EnsureLabels(ctx context.Context, labels []string) ([]string, error) {
	out, err := f.runJSON(ctx, "label", "list", "--limit", "1000", "--json", "name")
	if err != nil {
		return nil, err
	}
	var listed []ghLabel
	if err := json.Unmarshal(out, &listed); err != nil {
		return nil, fmt.Errorf("gh label list: parse: %w", err)
	}
	existing := make(map[string]struct{}, len(listed))
	for _, label := range listed {
		existing[label.Name] = struct{}{}
	}
	var created []string
	for _, label := range labels {
		if _, ok := existing[label]; ok {
			continue
		}
		if _, err := f.run(ctx, "label", "create", label); err != nil {
			return created, err
		}
		created = append(created, label)
	}
	return created, nil
}

type ghPermission struct {
	Permission string `json:"permission"`
}

// CanWrite reports whether login may trigger the agent on repo. Anything other
// than write or admin is a no: a stranger who can open an issue on a public
// repository must not be able to start a run on it.
func (f *Forge) CanWrite(ctx context.Context, repo, login string) (bool, error) {
	out, err := f.runJSON(ctx, "api", fmt.Sprintf("repos/%s/collaborators/%s/permission", repo, login))
	if err != nil {
		// GitHub answers 404 for a login with no access at all, which is a
		// definitive no rather than a failure to determine.
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return false, nil
		}
		return false, err
	}
	var p ghPermission
	if err := json.Unmarshal(out, &p); err != nil {
		return false, fmt.Errorf("gh api permission: parse: %w", err)
	}
	return p.Permission == "write" || p.Permission == "admin", nil
}

type ghViewer struct {
	Data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	} `json:"data"`
}

// Whoami returns the login the current credential acts as.
func (f *Forge) Whoami(ctx context.Context) (string, error) {
	out, err := f.runJSON(ctx, "api", "graphql", "-f", "query=query { viewer { login } }")
	if err != nil {
		return "", err
	}
	var v ghViewer
	if err := json.Unmarshal(out, &v); err != nil {
		return "", fmt.Errorf("gh api graphql viewer: parse: %w", err)
	}
	if v.Data.Viewer.Login == "" {
		return "", fmt.Errorf("gh api graphql viewer: empty login")
	}
	return v.Data.Viewer.Login, nil
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
// IsPullRequest asks the issues endpoint, which answers for both surfaces and
// carries a pull_request object only when the number is a pull request. That is
// one call and it is how GitHub itself draws the line.
func (f *Forge) IsPullRequest(ctx context.Context, repo string, number int) (bool, error) {
	out, err := f.runJSON(ctx, "api", fmt.Sprintf("repos/%s/issues/%d", repo, number))
	if err != nil {
		return false, err
	}
	var probe struct {
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(out, &probe); err != nil {
		return false, fmt.Errorf("gh api issue: parse: %w", err)
	}
	return probe.PullRequest != nil, nil
}

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
