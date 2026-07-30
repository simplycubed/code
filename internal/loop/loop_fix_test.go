package loop

import (
	"context"
	"strings"
	"testing"

	enginefake "github.com/simplycubed/code/internal/engine/fake"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
	"github.com/simplycubed/code/internal/state"
)

func newFixEngine(dir string, v VCS, r *enginefake.Runner) (*Engine, *forgefake.Forge) {
	f := &forgefake.Forge{}
	return &Engine{
		Runner: r,
		Gate:   gateChecksFile(),
		Forge:  f,
		VCS:    v,
		Cfg:    Config{WorkDir: dir, Branch: "pr-branch", Attribute: true},
	}, f
}

func fixReq() FixRequest {
	return FixRequest{Repo: "o/r", PR: 42, Issue: 7, Branch: "pr-branch", Prompt: "address the feedback"}
}

// The happy path: the fixer turns the feedback into a change that passes the
// gate, the change is committed and pushed to the pull request's own branch (not
// a new PR), the pull request is commented on, and the issue returns to review.
func TestFixPushesToBranchAndReRequestsReview(t *testing.T) {
	dir := t.TempDir()
	v := &fakeVCS{committed: true}
	eng, f := newFixEngine(dir, v, enginefake.New(
		enginefake.Step{Summary: "addressed", Apply: writeFixed},
	))

	res, err := eng.Fix(context.Background(), fixReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeChangesPushed {
		t.Fatalf("outcome = %s want changes_pushed (reason %s)", res.Outcome, res.Reason)
	}
	if f.PRCount != 0 {
		t.Fatalf("the fix loop must not open a new PR; opened %d", f.PRCount)
	}
	if v.pushedBranch != "pr-branch" {
		t.Fatalf("pushed to %q want the PR branch pr-branch", v.pushedBranch)
	}
	if !strings.Contains(v.commitMsg, "Address review feedback") {
		t.Fatalf("commit message = %q", v.commitMsg)
	}
	// Attribution on by default: the commit carries the co-author trailer.
	if !strings.Contains(v.commitMsg, "SimplyCubed Code") {
		t.Fatalf("expected attribution trailer in commit message: %q", v.commitMsg)
	}
	if len(f.PRComments) != 1 || !strings.Contains(f.PRComments[0], "Ready for another look") {
		t.Fatalf("expected an ack comment on the PR, got %v", f.PRComments)
	}
	// State goes on the linked issue (7), back to review.
	if !f.SawState(state.Label("sc", state.Review)) {
		t.Fatal("expected the review label after pushing the fix")
	}
}

// Negative control: feedback the fixer cannot satisfy. The gate stays red the
// same way twice, so the loop stalls, blocks, and pushes nothing.
func TestFixUnsatisfiableBlocksWithoutPush(t *testing.T) {
	dir := t.TempDir()
	v := &fakeVCS{committed: true}
	eng, f := newFixEngine(dir, v, enginefake.New(
		enginefake.Step{Summary: "tried"},
		enginefake.Step{Summary: "tried again"},
		enginefake.Step{Summary: "still stuck"},
	))

	res, _ := eng.Fix(context.Background(), fixReq())
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if v.pushDone {
		t.Fatal("a blocked fix must not push")
	}
	if !f.SawState(state.Label("sc", state.Blocked)) {
		t.Fatal("expected the blocked label")
	}
	if len(f.PRComments) == 0 || !strings.Contains(f.PRComments[0], "Blocked") {
		t.Fatalf("expected a blocked comment on the PR, got %v", f.PRComments)
	}
}

// The gate passes but the fixer produced no diff: it could not turn the feedback
// into a concrete edit. That is a human's problem, so it blocks and pushes
// nothing rather than pushing an empty change.
func TestFixNoChangeBlocksWithoutPush(t *testing.T) {
	dir := t.TempDir()
	v := &fakeVCS{committed: false}
	eng, _ := newFixEngine(dir, v, enginefake.New(
		enginefake.Step{Summary: "no-op", Apply: writeFixed},
	))

	res, _ := eng.Fix(context.Background(), fixReq())
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if v.pushDone {
		t.Fatal("must not push when there is no change")
	}
	if !strings.Contains(res.Reason, "no change") {
		t.Fatalf("reason = %q, expected it to mention no change", res.Reason)
	}
}

// When there is no linked issue in the PR title, state labels fall back to the
// PR number itself rather than being dropped.
func TestFixStateFallsBackToPRWhenNoIssue(t *testing.T) {
	dir := t.TempDir()
	v := &fakeVCS{committed: true}
	eng, f := newFixEngine(dir, v, enginefake.New(
		enginefake.Step{Summary: "addressed", Apply: writeFixed},
	))
	req := fixReq()
	req.Issue = 0 // no linked issue

	if _, err := eng.Fix(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.SawState(state.Label("sc", state.Review)) {
		t.Fatal("expected the review label to be set on the PR when there is no linked issue")
	}
}

func TestFixWorkflowChangesBlockBeforeTheGateWhenAppCannotPushThem(t *testing.T) {
	dir := t.TempDir()
	v := &fakeVCS{workflows: true}
	eng, f := newFixEngine(dir, v, enginefake.New(
		enginefake.Step{Summary: "updated the caller workflow", Apply: writeFixed},
	))
	eng.Cfg.WorkflowRestrictedPush = true

	res, err := eng.Fix(context.Background(), fixReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if v.commitDone || v.pushDone {
		t.Fatalf("workflow pre-flight must block before commit/push; commit=%v push=%v", v.commitDone, v.pushDone)
	}
	if len(f.PRComments) == 0 || !strings.Contains(f.PRComments[0], "lacks `workflows` permission") {
		t.Fatalf("expected actionable PR comment, got %v", f.PRComments)
	}
}
