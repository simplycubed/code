// Package fake is a recording forge.Forge for tests. It performs no GitHub calls
// and records what the loop asked it to do, so a test can assert (for example)
// that no pull request was opened on a blocked run.
package fake

import (
	"context"
	"slices"
	"sync"

	"github.com/simplycubed/code/internal/domain"
)

// Forge records calls made to it.
type Forge struct {
	mu         sync.Mutex
	PRCount    int
	PRBodies   []string
	States     []string
	Comments   []string
	PRComments []string
	// URL is returned by OpenPR; a default is used if empty.
	URL string
	// Feedbacks is a scripted queue returned by successive Feedback calls; the
	// last entry is repeated once the queue is drained. A zero value returns
	// empty feedback (nothing to address).
	Feedbacks []domain.ReviewFeedback
	fbCalls   int
}

// OpenPR records a pull request and returns a URL.
func (f *Forge) OpenPR(_ context.Context, _, _, _, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PRCount++
	f.PRBodies = append(f.PRBodies, body)
	if f.URL != "" {
		return f.URL, nil
	}
	return "https://example.test/pr/1", nil
}

// SetState records a state-label application.
func (f *Forge) SetState(_ context.Context, _ string, _ int, label string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.States = append(f.States, label)
	return nil
}

// Comment records a comment.
func (f *Forge) Comment(_ context.Context, _ string, _ int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Comments = append(f.Comments, body)
	return nil
}

// CommentPR records a pull-request comment.
func (f *Forge) CommentPR(_ context.Context, _ string, _ int, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PRComments = append(f.PRComments, body)
	return nil
}

// Feedback returns the next scripted ReviewFeedback, repeating the last one once
// the queue is drained. With no scripted feedback it returns an empty value,
// which the loop reads as "nothing to address".
func (f *Forge) Feedback(_ context.Context, _ string, pr int) (domain.ReviewFeedback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Feedbacks) == 0 {
		return domain.ReviewFeedback{PR: pr}, nil
	}
	i := f.fbCalls
	if i >= len(f.Feedbacks) {
		i = len(f.Feedbacks) - 1
	}
	f.fbCalls++
	return f.Feedbacks[i], nil
}

// SawState reports whether a label was ever set.
func (f *Forge) SawState(label string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.States, label)
}
