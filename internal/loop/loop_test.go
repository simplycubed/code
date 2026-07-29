package loop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
	enginefake "github.com/simplycubed/code/internal/engine/fake"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
	"github.com/simplycubed/code/internal/gate"
	"github.com/simplycubed/code/internal/state"
)

// gateChecksFile passes if and only if workDir/fixed exists. Until then it
// returns a red result with a constant signature, which is what the loop's
// stall detection keys on.
func gateChecksFile() GateFunc {
	return func(_ context.Context, workDir string) gate.Result {
		if _, err := os.Stat(filepath.Join(workDir, "fixed")); err == nil {
			return gate.Result{Passed: true}
		}
		return gate.Result{Passed: false, ExitCode: 1, OutputTail: "FAIL", Signature: "REDSIG"}
	}
}

func writeFixed(workDir string) error {
	return os.WriteFile(filepath.Join(workDir, "fixed"), []byte("ok"), 0o644)
}

func newEngine(dir string, r *enginefake.Runner) (*Engine, *forgefake.Forge) {
	f := &forgefake.Forge{}
	return &Engine{
		Runner: r,
		Gate:   gateChecksFile(),
		Forge:  f,
		Cfg:    Config{WorkDir: dir, Branch: "loop/7"},
	}, f
}

func TestSuccessOpensPR(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "fix", Apply: writeFixed},
	))
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s want pr_opened", res.Outcome)
	}
	if f.PRCount != 1 {
		t.Fatalf("PRCount = %d want 1", f.PRCount)
	}
	if !f.SawState(state.Label("sc", state.Review)) {
		t.Fatal("expected the review label to be set when the PR opens")
	}
}

func TestFixOnSecondRoundOpensPR(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "looked, no change"},
		enginefake.Step{Summary: "fix", Apply: writeFixed},
	))
	res, _ := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if res.Outcome != OutcomePROpened || res.Rounds != 2 {
		t.Fatalf("outcome=%s rounds=%d want pr_opened/2", res.Outcome, res.Rounds)
	}
	if f.PRCount != 1 {
		t.Fatalf("PRCount = %d want 1", f.PRCount)
	}
}

// The honesty test. An engine that never fixes the gate must end Blocked and
// must open no pull request. A loop that only ever succeeds in tests is
// indistinguishable from a loop with no stop condition.
func TestHonestyStallBlocksAndOpensNoPR(t *testing.T) {
	dir := t.TempDir()
	// Steps that never write "fixed": the gate stays red with the same signature.
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "tried"},
		enginefake.Step{Summary: "tried again"},
		enginefake.Step{Summary: "still trying"},
	))
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if err != nil {
		t.Fatalf("stall should not be an error: %v", err)
	}
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if f.PRCount != 0 {
		t.Fatalf("a blocked run opened %d PRs; must be 0", f.PRCount)
	}
	if !f.SawState(state.Label("sc", state.Blocked)) {
		t.Fatal("expected the blocked label to be set")
	}
	if !strings.Contains(res.Reason, "stall") {
		t.Fatalf("reason = %q, expected it to mention the stall", res.Reason)
	}
}

func TestEngineErrorBlocksAndOpensNoPR(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Err: errors.New("engine crashed")},
	))
	res, _ := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s want blocked", res.Outcome)
	}
	if f.PRCount != 0 {
		t.Fatalf("engine error opened %d PRs; must be 0", f.PRCount)
	}
	if !strings.Contains(res.Reason, "engine error") {
		t.Fatalf("reason = %q, expected engine error", res.Reason)
	}
}
