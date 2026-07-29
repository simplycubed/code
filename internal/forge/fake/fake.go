// Package fake is a recording forge.Forge for tests. It performs no GitHub calls
// and records what the loop asked it to do, so a test can assert (for example)
// that no pull request was opened on a blocked run.
package fake

import (
	"context"
	"sync"
)

// Forge records calls made to it.
type Forge struct {
	mu       sync.Mutex
	PRCount  int
	States   []string
	Comments []string
	// URL is returned by OpenPR; a default is used if empty.
	URL string
}

// OpenPR records a pull request and returns a URL.
func (f *Forge) OpenPR(_ context.Context, _, _, _, _ string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.PRCount++
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

// SawState reports whether a label was ever set.
func (f *Forge) SawState(label string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.States {
		if s == label {
			return true
		}
	}
	return false
}
