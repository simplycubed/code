package loop

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
	enginefake "github.com/simplycubed/code/internal/engine/fake"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
)

type fakeVCS struct {
	committed    bool
	commitDone   bool
	commitErr    error
	pushDone     bool
	pushErr      error
	pushedBranch string
	syncedBranch string
	workflows    bool
	workflowErr  error
	commitMsg    string
}

func (f *fakeVCS) Commit(_ context.Context, _, msg string) (bool, error) {
	f.commitDone = true
	f.commitMsg = msg
	return f.committed, f.commitErr
}

func (f *fakeVCS) Push(_ context.Context, _, branch string) error {
	f.pushDone = true
	f.pushedBranch = branch
	return f.pushErr
}

func (f *fakeVCS) Sync(_ context.Context, _, branch string) error {
	f.syncedBranch = branch
	return nil
}

func (f *fakeVCS) TouchesWorkflow(_ context.Context, _ string) (bool, error) {
	return f.workflows, f.workflowErr
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

func TestWorkflowChangesBlockBeforeTheGateWhenAppCannotPushThem(t *testing.T) {
	dir := t.TempDir()
	f := &forgefake.Forge{}
	v := &fakeVCS{workflows: true}
	eng := &Engine{
		Runner: enginefake.New(enginefake.Step{Summary: "updated the workflow", Apply: writeFixed}),
		Gate:   gateChecksFile(),
		Forge:  f,
		VCS:    v,
		Cfg:    Config{WorkDir: dir, Branch: "loop/1", WorkflowRestrictedPush: true},
	}

	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if v.commitDone || v.pushDone {
		t.Fatalf("workflow pre-flight must block before commit/push; commit=%v push=%v", v.commitDone, v.pushDone)
	}
	if f.PRCount != 0 {
		t.Fatalf("PRCount = %d want 0", f.PRCount)
	}
	for _, want := range []string{".github/workflows/", "lacks `workflows` permission", "human", "own GitHub auth"} {
		if !strings.Contains(res.Reason, want) {
			t.Fatalf("reason = %q, expected %q", res.Reason, want)
		}
	}
}

func TestWorkflowPushRefusalEscalatesClearly(t *testing.T) {
	dir := t.TempDir()
	f := &forgefake.Forge{}
	v := &fakeVCS{
		committed: true,
		pushErr:   errors.New("git push: remote: error: refusing to allow a GitHub App to create or update workflow `.github/workflows/check.yml` without `workflows` permission"),
	}
	eng := &Engine{
		Runner: enginefake.New(enginefake.Step{Summary: "updated the workflow", Apply: writeFixed}),
		Gate:   gateChecksFile(),
		Forge:  f,
		VCS:    v,
		Cfg:    Config{WorkDir: dir, Branch: "loop/1"},
	}

	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if !v.commitDone || !v.pushDone {
		t.Fatalf("expected commit and push attempt; commit=%v push=%v", v.commitDone, v.pushDone)
	}
	if strings.Contains(res.Reason, "push failed:") {
		t.Fatalf("reason should be rewritten, got %q", res.Reason)
	}
	if !strings.Contains(res.Reason, "lacks `workflows` permission") {
		t.Fatalf("reason = %q", res.Reason)
	}
}
