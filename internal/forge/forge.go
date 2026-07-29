// Package forge is the GitHub side of the loop: opening pull requests, setting
// state labels, and commenting. It is an interface so the loop can be tested
// without touching GitHub, and so the single real implementation (via the gh CLI
// or the API) is swappable.
//
// Note what is deliberately absent: there is no Merge method. The loop cannot
// merge, by construction. The strongest thing it can do here is open a pull
// request.
package forge

import "context"

// Forge is the subset of GitHub operations the loop performs.
type Forge interface {
	// OpenPR opens a pull request from branch and returns its URL.
	OpenPR(ctx context.Context, repo, branch, title, body string) (url string, err error)
	// SetState applies a state label to an issue. The caller is responsible for
	// removing the prior state label so exactly one remains.
	SetState(ctx context.Context, repo string, issue int, label string) error
	// Comment posts a comment on an issue.
	Comment(ctx context.Context, repo string, issue int, body string) error
}
