// Package dryrun wraps a forge so a run exercises everything except the parts
// that change someone's repository.
//
// Reads pass through, because a dry run that cannot see the issue or the review
// feedback is not exercising the same code path. Writes are recorded and
// skipped: no pull request, no comment, no label. The run still creates a
// worktree, calls the engine, and grades against the real gate, so the expensive
// and failure-prone parts are all covered.
//
// The point is debuggability. Every failure found in GitHub Actions so far was
// invisible until a real run had already half-happened; a dry run makes the same
// path observable with nothing at stake.
package dryrun

import (
	"context"
	"fmt"
	"strings"

	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/forge"
)

// Action is one write the run would have made.
type Action struct {
	Kind   string // "open-pr", "comment", "comment-pr", "set-state"
	Target string // repo, plus the issue or pull request it applies to
	Detail string // the label, or the body that would have been posted
}

// Forge decorates another forge, recording writes instead of performing them.
type Forge struct {
	Inner forge.Forge
	// URL is returned by OpenPR in place of a real one, so the loop's control
	// flow continues exactly as it would have.
	URL string

	actions []Action
}

// New wraps inner.
func New(inner forge.Forge) *Forge {
	return &Forge{Inner: inner, URL: "https://example.invalid/dry-run/pull/0"}
}

func (f *Forge) record(kind, target, detail string) {
	f.actions = append(f.actions, Action{Kind: kind, Target: target, Detail: detail})
}

// Actions returns the writes the run would have made, in order.
func (f *Forge) Actions() []Action { return f.actions }

// OpenPR records the pull request and returns a placeholder URL.
func (f *Forge) OpenPR(_ context.Context, repo, branch, title, body string) (string, error) {
	f.record("open-pr", fmt.Sprintf("%s (%s)", repo, branch), title+"\n\n"+body)
	return f.URL, nil
}

// SetState records the label that would have been applied.
func (f *Forge) SetState(_ context.Context, repo string, issue int, label string) error {
	f.record("set-state", fmt.Sprintf("%s#%d", repo, issue), label)
	return nil
}

// Comment records the comment that would have been posted.
func (f *Forge) Comment(_ context.Context, repo string, issue int, body string) error {
	f.record("comment", fmt.Sprintf("%s#%d", repo, issue), body)
	return nil
}

// CommentPR records the pull-request comment that would have been posted.
func (f *Forge) CommentPR(_ context.Context, repo string, pr int, body string) error {
	f.record("comment-pr", fmt.Sprintf("%s#%d", repo, pr), body)
	return nil
}

// Feedback reads through: a dry run has to see the same input as a real one.
func (f *Forge) Feedback(ctx context.Context, repo string, pr int) (domain.ReviewFeedback, error) {
	return f.Inner.Feedback(ctx, repo, pr)
}

// CanWrite reads through, so authorization is exercised rather than assumed.
func (f *Forge) CanWrite(ctx context.Context, repo, login string) (bool, error) {
	return f.Inner.CanWrite(ctx, repo, login)
}

// Whoami reads through.
func (f *Forge) Whoami(ctx context.Context) (string, error) { return f.Inner.Whoami(ctx) }

// Report renders the recorded writes for a human reading a run log.
func (f *Forge) Report() string {
	if len(f.actions) == 0 {
		return "DRY RUN: the loop made no GitHub writes."
	}
	var b strings.Builder
	b.WriteString("DRY RUN: the following GitHub writes were skipped.\n")
	for i, a := range f.actions {
		fmt.Fprintf(&b, "\n%d. %s -> %s\n", i+1, a.Kind, a.Target)
		for _, line := range strings.Split(strings.TrimRight(a.Detail, "\n"), "\n") {
			fmt.Fprintf(&b, "   | %s\n", line)
		}
	}
	return b.String()
}

// IsPullRequest passes through. It is a read, and a dry run that cannot tell an
// issue from a pull request would take a different branch than the real thing.
func (f *Forge) IsPullRequest(ctx context.Context, repo string, number int) (bool, error) {
	return f.Inner.IsPullRequest(ctx, repo, number)
}
