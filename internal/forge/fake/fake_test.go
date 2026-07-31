package fake

import (
	"context"
	"errors"
	"testing"
)

// The fake answers the surface question from a small map, so a test can put a
// command on an issue or a pull request without a network.
func TestIsPullRequest(t *testing.T) {
	ctx := context.Background()
	f := &Forge{PullRequests: map[int]bool{7: true}}
	for number, want := range map[int]bool{7: true, 8: false} {
		got, err := f.IsPullRequest(ctx, "o/r", number)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("IsPullRequest(%d) = %v, want %v", number, got, want)
		}
	}

	// A scripted failure, so a caller's error path is reachable in a test.
	want := errors.New("nope")
	f = &Forge{IsPRErr: want}
	if _, err := f.IsPullRequest(ctx, "o/r", 1); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
