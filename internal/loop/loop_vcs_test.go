package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
	enginefake "github.com/simplycubed/code/internal/engine/fake"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
)

type fakeVCS struct {
	committed    bool
	commitDone   bool
	pushDone     bool
	pushedBranch string
	syncedBranch string
	commitMsg    string
}

func (f *fakeVCS) Commit(_ context.Context, _, msg string) (bool, error) {
	f.commitDone = true
	f.commitMsg = msg
	return f.committed, nil
}

func (f *fakeVCS) Push(_ context.Context, _, branch string) error {
	f.pushDone = true
	f.pushedBranch = branch
	return nil
}

func (f *fakeVCS) Sync(_ context.Context, _, branch string) error {
	f.syncedBranch = branch
	return nil
}

func TestOpenPRCommitsAndPushesFirst(t *testing.T) {
	dir := t.TempDir()
	f := &forgefake.Forge{}
	v := &fakeVCS{committed: true}
	eng := &Engine{
		Runner: enginefake.New(enginefake.Step{Summary: "fix", Apply: writeFixed}),
		Gate:   gateChecksFile(),
		Forge:  f,
		VCS:    v,
		Cfg:    Config{WorkDir: dir, Branch: "loop/1"},
	}
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s want pr_opened", res.Outcome)
	}
	if !v.commitDone || !v.pushDone {
		t.Fatalf("expected commit and push before the PR; commit=%v push=%v", v.commitDone, v.pushDone)
	}
	if f.PRCount != 1 {
		t.Fatalf("PRCount = %d want 1", f.PRCount)
	}
}

// The gate passed but the working tree has nothing to commit: there is nothing to
// propose, so the run blocks and opens no PR.
func TestNoChangesToProposeBlocksWithoutPR(t *testing.T) {
	dir := t.TempDir()
	f := &forgefake.Forge{}
	v := &fakeVCS{committed: false}
	eng := &Engine{
		Runner: enginefake.New(enginefake.Step{Summary: "fix", Apply: writeFixed}),
		Gate:   gateChecksFile(),
		Forge:  f,
		VCS:    v,
		Cfg:    Config{WorkDir: dir, Branch: "loop/1"},
	}
	res, _ := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if f.PRCount != 0 {
		t.Fatalf("PRCount = %d want 0", f.PRCount)
	}
	if !strings.Contains(res.Reason, "no changes") {
		t.Fatalf("reason = %q, expected it to mention no changes", res.Reason)
	}
}
