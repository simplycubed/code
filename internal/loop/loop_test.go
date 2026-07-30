package loop

import (
	"bytes"
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
	"github.com/simplycubed/code/internal/ledger"
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

// The Describe hook's output lands in the pull-request body; an empty result
// leaves the plain body. Either way the PR opens — the hook is additive only.
func TestDescribeHookEnrichesPRBody(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "fix", Apply: writeFixed},
	))
	eng.Describe = func(context.Context, domain.Issue) string { return "## Walkthrough\n\nrich section" }
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if err != nil || res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if len(f.PRBodies) != 1 || !strings.Contains(f.PRBodies[0], "rich section") {
		t.Fatalf("PR body missing the described section: %q", f.PRBodies)
	}
	if !strings.Contains(f.PRBodies[0], "Automated change from an issue") {
		t.Fatalf("plain body line must remain: %q", f.PRBodies[0])
	}
}

func TestDescribeHookEmptyKeepsPlainBody(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "fix", Apply: writeFixed},
	))
	eng.Describe = func(context.Context, domain.Issue) string { return "" }
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7})
	if err != nil || res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if len(f.PRBodies) != 1 || f.PRBodies[0] != "Automated change from an issue. A human reviews and merges; this loop does not." {
		t.Fatalf("plain body expected: %q", f.PRBodies)
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

func TestLedgerRecordsRoundsAndOutcome(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	eng, _ := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "looked, no change"},
		enginefake.Step{Summary: "fix", Apply: writeFixed},
	))
	eng.Ledger = ledger.New(&buf)
	eng.Cfg.RunID = "run-abc"

	if _, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// two round lines (fail then pass) plus one run_end line.
	if len(lines) != 3 {
		t.Fatalf("got %d ledger lines want 3:\n%s", len(lines), out)
	}
	if !strings.Contains(out, "run-abc") {
		t.Fatal("ledger missing run id")
	}
	if !strings.Contains(lines[0], "\"gate\":\"fail\"") {
		t.Fatalf("first round should be fail: %s", lines[0])
	}
	if !strings.Contains(lines[2], "\"outcome\":\"pr_opened\"") {
		t.Fatalf("run_end should be pr_opened: %s", lines[2])
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

// An escalation that says only "no changes to propose" leaves a human with
// nothing to act on. The engine's closing message is the agent's own account of
// what it decided, so it has to reach the issue.
func TestEscalationCarriesTheEngineSummary(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "STATUS.md already reads correctly to me, so I changed nothing."},
		enginefake.Step{Summary: "STATUS.md already reads correctly to me, so I changed nothing."},
		enginefake.Step{Summary: "STATUS.md already reads correctly to me, so I changed nothing."},
	))
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 55})
	if err != nil || res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if len(f.Comments) == 0 {
		t.Fatal("expected a comment on the issue")
	}
	got := f.Comments[len(f.Comments)-1]
	if !strings.Contains(got, "STATUS.md already reads correctly") {
		t.Fatalf("escalation should carry the agent's account, got:\n%s", got)
	}
	// The reason itself must survive alongside it.
	if !strings.Contains(got, "Blocked:") {
		t.Fatalf("escalation should still state the reason, got:\n%s", got)
	}
}

func TestEscalationWithoutASummaryIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{}, enginefake.Step{}, enginefake.Step{},
	))
	if _, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 7}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := f.Comments[len(f.Comments)-1]
	if strings.Contains(got, "What the agent reported") {
		t.Fatalf("an empty summary must not add an empty section, got:\n%s", got)
	}
}

// The reviewer decides whether a human ever sees the pull request, so each
// branch is pinned: a trusted pass opens it, findings go to the fixer first, a
// contradictory verdict is not trusted, and silence is not approval.
func TestReviewOpensThePRWhenTheVerdictIsTrusted(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(enginefake.Step{Summary: "fix", Apply: writeFixed}))
	eng.Review = func(context.Context, domain.Issue) (domain.Verdict, bool) {
		return domain.Verdict{Pass: true, Summary: "Looks right."}, true
	}
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
	if err != nil || res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if !strings.Contains(f.PRBodies[0], "Looks right.") {
		t.Fatalf("the PR body should carry the review summary: %q", f.PRBodies[0])
	}
}

func TestReviewSendsFindingsToTheFixerBeforeOpening(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "fix", Apply: writeFixed},
		enginefake.Step{Summary: "second round"},
	))
	calls := 0
	eng.Review = func(context.Context, domain.Issue) (domain.Verdict, bool) {
		calls++
		if calls == 1 {
			return domain.Verdict{Pass: false, Findings: []domain.Finding{
				{Severity: domain.SeverityBlocker, Problem: "nil deref", Required: "check it"},
			}}, true
		}
		return domain.Verdict{Pass: true, Summary: "Fixed."}, true
	}
	var got []domain.ReviewNote
	eng.FixFindings = func(_ context.Context, _ domain.Issue, notes []domain.ReviewNote) error {
		got = notes
		return nil
	}
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 2})
	if err != nil || res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if len(got) != 1 || !strings.Contains(got[0].Body, "nil deref") {
		t.Fatalf("the fixer should receive the findings, got %+v", got)
	}
	if f.PRCount != 1 {
		t.Fatalf("PRCount = %d, want one PR opened after the fix", f.PRCount)
	}
}

// A verdict claiming pass while reporting a blocker contradicts itself. The
// loop takes the pessimistic reading and does not open the pull request.
func TestReviewDoesNotTrustAPassCarryingABlocker(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(
		enginefake.Step{Summary: "fix", Apply: writeFixed},
		enginefake.Step{Summary: "again"},
	))
	eng.Review = func(context.Context, domain.Issue) (domain.Verdict, bool) {
		return domain.Verdict{Pass: true, Findings: []domain.Finding{
			{Severity: domain.SeverityBlocker, Problem: "unsafe"},
		}}, true
	}
	fixed := false
	eng.FixFindings = func(context.Context, domain.Issue, []domain.ReviewNote) error { fixed = true; return nil }
	_, _ = eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 3})
	if !fixed {
		t.Fatal("a contradictory verdict must reach the fixer, not the human")
	}
	if f.PRCount != 0 {
		t.Fatal("no pull request should open while a blocker stands")
	}
}

// No usable verdict is an absent judgment. It must not block the change
// forever, and it must not be read as approval either: the human decides.
func TestReviewFallsBackToTheHumanWhenNoVerdictIsProduced(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(enginefake.Step{Summary: "fix", Apply: writeFixed}))
	eng.Review = func(context.Context, domain.Issue) (domain.Verdict, bool) {
		return domain.Verdict{}, false
	}
	res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 4})
	if err != nil || res.Outcome != OutcomePROpened {
		t.Fatalf("outcome = %s err = %v", res.Outcome, err)
	}
	if f.PRCount != 1 {
		t.Fatal("the change should still reach a human")
	}
}

func TestReviewEscalatesWhenItWithholdsAPassWithoutFindings(t *testing.T) {
	dir := t.TempDir()
	eng, f := newEngine(dir, enginefake.New(enginefake.Step{Summary: "fix", Apply: writeFixed}))
	eng.Review = func(context.Context, domain.Issue) (domain.Verdict, bool) {
		return domain.Verdict{Pass: false}, true
	}
	res, _ := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 5})
	if res.Outcome != OutcomeBlocked {
		t.Fatalf("outcome = %s, want blocked", res.Outcome)
	}
	if f.PRCount != 0 {
		t.Fatal("no pull request should open")
	}
}
