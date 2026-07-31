package dryrun

import (
	"context"
	"strings"
	"testing"

	forgefake "github.com/simplycubed/code/internal/forge/fake"
)

func TestWritesAreRecordedNotPerformed(t *testing.T) {
	inner := &forgefake.Forge{}
	f := New(inner)
	ctx := context.Background()

	if _, err := f.OpenPR(ctx, "o/r", "sc/1", "Closes #1: t", "body"); err != nil {
		t.Fatal(err)
	}
	_ = f.SetState(ctx, "o/r", 1, "sc:review")
	_ = f.Comment(ctx, "o/r", 1, "blocked: x")
	_ = f.CommentPR(ctx, "o/r", 2, "ready")

	// Nothing may reach the real forge.
	if inner.PRCount != 0 || len(inner.States) != 0 || len(inner.Comments) != 0 || len(inner.PRComments) != 0 {
		t.Fatalf("a dry run must not write: PRs=%d states=%v comments=%v prComments=%v",
			inner.PRCount, inner.States, inner.Comments, inner.PRComments)
	}
	if len(f.Actions()) != 4 {
		t.Fatalf("expected 4 recorded actions, got %d", len(f.Actions()))
	}
}

// Reads have to pass through, or a dry run is not exercising the same path.
func TestReadsPassThrough(t *testing.T) {
	inner := &forgefake.Forge{Writers: map[string]bool{"maintainer": true}, Login: "bot[bot]"}
	f := New(inner)
	ctx := context.Background()

	ok, err := f.CanWrite(ctx, "o/r", "maintainer")
	if err != nil || !ok {
		t.Fatalf("CanWrite should read through: %v %v", ok, err)
	}
	who, err := f.Whoami(ctx)
	if err != nil || who != "bot[bot]" {
		t.Fatalf("Whoami should read through: %q %v", who, err)
	}
	if _, err := f.Feedback(ctx, "o/r", 1); err != nil {
		t.Fatalf("Feedback should read through: %v", err)
	}
}

// The report is the entire product of a dry run, so it has to name what would
// have happened and to what.
func TestReportNamesEachSkippedWrite(t *testing.T) {
	f := New(&forgefake.Forge{})
	ctx := context.Background()
	_, _ = f.OpenPR(ctx, "o/r", "sc/7", "Closes #7: title", "the body")
	_ = f.SetState(ctx, "o/r", 7, "sc:review")

	got := f.Report()
	for _, want := range []string{"DRY RUN", "open-pr", "o/r (sc/7)", "Closes #7: title", "the body", "set-state", "sc:review"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
}

func TestReportSaysSoWhenNothingHappened(t *testing.T) {
	got := New(&forgefake.Forge{}).Report()
	if !strings.Contains(got, "no GitHub writes") {
		t.Fatalf("report should state that nothing happened: %s", got)
	}
}

// IsPullRequest is a read, so a dry run must see the same answer the real run
// would. Getting this wrong would send a dry run down a different branch than
// the thing it is meant to preview.
func TestIsPullRequestPassesThrough(t *testing.T) {
	inner := &forgefake.Forge{PullRequests: map[int]bool{7: true}}
	d := New(inner)
	for number, want := range map[int]bool{7: true, 8: false} {
		got, err := d.IsPullRequest(context.Background(), "o/r", number)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != want {
			t.Fatalf("IsPullRequest(%d) = %v, want %v", number, got, want)
		}
	}
	if len(d.Actions()) != 0 {
		t.Fatalf("a read must not be recorded as a write, recorded: %v", d.Actions())
	}
}
