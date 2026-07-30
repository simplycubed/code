package loop_test

// An end-to-end composition test: the loop engine wired to the real gate runner
// (internal/gate), a fake engine standing in for the model, and a recording fake
// forge. It proves the pieces fit together and that a green real-gate run ends in
// a pull request. The model adapter (codex) and the GitHub adapter (gh) have
// their own tests; this covers the seams between loop, gate, and forge.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/simplycubed/code/internal/domain"
	enginefake "github.com/simplycubed/code/internal/engine/fake"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
	"github.com/simplycubed/code/internal/gate"
	"github.com/simplycubed/code/internal/loop"
)

func TestLoopComposesWithRealGateRunner(t *testing.T) {
	dir := t.TempDir()

	// The real gate: a shell command run by internal/gate, passing only once the
	// engine has written the "fixed" marker into the working tree.
	realGate := func(ctx context.Context, workDir string) gate.Result {
		return gate.Run(ctx, workDir, "test -f fixed")
	}

	forge := &forgefake.Forge{}
	eng := &loop.Engine{
		Runner: enginefake.New(
			// round 1: no change, so the real gate fails
			enginefake.Step{Summary: "looked"},
			// round 2: write the marker, so the gate passes
			enginefake.Step{Summary: "fixed", Apply: func(wd string) error {
				return os.WriteFile(filepath.Join(wd, "fixed"), []byte("x"), 0o644)
			}},
		),
		Gate:  realGate,
		Forge: forge,
		Cfg:   loop.Config{WorkDir: dir, Branch: "loop/1"},
	}

	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Outcome != loop.OutcomePROpened {
		t.Fatalf("outcome = %s want pr_opened (reason: %s)", res.Outcome, res.Reason)
	}
	if res.Rounds != 2 {
		t.Fatalf("rounds = %d want 2 (fail then pass)", res.Rounds)
	}
	if forge.PRCount != 1 {
		t.Fatalf("PRCount = %d want 1", forge.PRCount)
	}
}
