// Package forge is the GitHub side of the loop: opening pull requests, setting
// state labels, and commenting. It is an interface so the loop can be tested
// without touching GitHub, and so the single real implementation (via the gh CLI
// or the API) is swappable.
//
// Note what is deliberately absent: there is no Merge method. The loop cannot
// merge, by construction. The strongest thing it can do here is open a pull
// request.
package forge

import (
	"context"

	"github.com/simplycubed/code/internal/domain"
)

// Forge is the subset of GitHub operations the loop performs.
type Forge interface {
	// OpenPR opens a pull request from branch and returns its URL.
	OpenPR(ctx context.Context, repo, branch, title, body string) (url string, err error)
	// SetState applies a state label to an issue. The caller is responsible for
	// removing the prior state label so exactly one remains.
	SetState(ctx context.Context, repo string, issue int, label string) error
	// Comment posts a comment on an issue.
	Comment(ctx context.Context, repo string, issue int, body string) error
	// Feedback returns the actionable human review feedback on a pull request:
	// only feedback left against the current head, with the agent's own output
	// filtered out. An empty ReviewFeedback.Notes means there is nothing new to
	// address.
	Feedback(ctx context.Context, repo string, pr int) (domain.ReviewFeedback, error)
	// CommentPR posts a comment on a pull request. It is distinct from Comment
	// because pull requests and issues are different surfaces to the GitHub CLI.
	CommentPR(ctx context.Context, repo string, pr int, body string) error
	// CanWrite reports whether login has write or admin access to repo. It
	// answers "may this person trigger the agent", which is the security
	// question the runtime asks before doing anything else.
	CanWrite(ctx context.Context, repo, login string) (bool, error)
	// Whoami returns the login the current credential acts as. The loop uses it
	// to recognise its own output, so it never addresses its own review.
	Whoami(ctx context.Context) (string, error)
	// IsPullRequest reports whether a number is a pull request rather than a
	// plain issue. GitHub numbers both from one sequence, so a comment command
	// cannot tell from the number alone which verb applies to it.
	IsPullRequest(ctx context.Context, repo string, number int) (bool, error)
}
