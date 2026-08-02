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

// An adopter's escalation used to name our bot, so it pointed at an account
// they had never installed and a correct escalation read as a misconfiguration.
// Both paths that escalate must name the identity the run actually holds.
func TestWorkflowEscalationNamesTheAuthenticatedIdentity(t *testing.T) {
	t.Run("blocked before the gate", func(t *testing.T) {
		f := &forgefake.Forge{}
		eng := &Engine{
			Runner: enginefake.New(enginefake.Step{Summary: "updated the workflow", Apply: writeFixed}),
			Gate:   gateChecksFile(),
			Forge:  f,
			VCS:    &fakeVCS{workflows: true},
			Cfg: Config{
				WorkDir:                t.TempDir(),
				Branch:                 "loop/1",
				WorkflowRestrictedPush: true,
				SelfLogin:              "acme-code[bot]",
			},
		}

		res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Outcome != OutcomeBlocked {
			t.Fatalf("outcome = %s want blocked", res.Outcome)
		}
		if !strings.Contains(res.Reason, "`acme-code[bot]` GitHub App") {
			t.Fatalf("reason = %q, expected it to name the authenticated identity", res.Reason)
		}
		if strings.Contains(res.Reason, "simplycubed-code[bot]") {
			t.Fatalf("reason = %q, must not name our App in an adopter's repository", res.Reason)
		}
	})

	t.Run("blocked by the push refusal", func(t *testing.T) {
		f := &forgefake.Forge{}
		eng := &Engine{
			Runner: enginefake.New(enginefake.Step{Summary: "updated the workflow", Apply: writeFixed}),
			Gate:   gateChecksFile(),
			Forge:  f,
			VCS: &fakeVCS{
				committed: true,
				pushErr:   errors.New("git push: remote: error: refusing to allow a GitHub App to create or update workflow `.github/workflows/check.yml` without `workflows` permission"),
			},
			Cfg: Config{WorkDir: t.TempDir(), Branch: "loop/1", SelfLogin: "acme-code[bot]"},
		}

		res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Outcome != OutcomeBlocked {
			t.Fatalf("outcome = %s want blocked", res.Outcome)
		}
		if !strings.Contains(res.Reason, "`acme-code[bot]` GitHub App") {
			t.Fatalf("reason = %q, expected it to name the authenticated identity", res.Reason)
		}
	})

	t.Run("says a GitHub App when the identity is unknown", func(t *testing.T) {
		if got := workflowPermissionReason(""); !strings.Contains(got, "authenticated as a GitHub App") {
			t.Fatalf("reason = %q, expected the generic form when no login is known", got)
		}
	})
}

func TestWorkflowPushReasonOnlyClaimsTheRefusalItRecognises(t *testing.T) {
	eng := &Engine{Cfg: Config{SelfLogin: "acme-code[bot]"}}

	if got := eng.workflowPushReason(nil); got != "" {
		t.Fatalf("a successful push must produce no reason, got %q", got)
	}
	if got := eng.workflowPushReason(errors.New("git push: remote: connection reset")); got != "" {
		t.Fatalf("an unrelated push failure must not be reported as a permission problem, got %q", got)
	}
}

// The preflight must only stop a run when a workflow file genuinely changed.
// A false positive escalates work that would have pushed cleanly and tells the
// operator to do it by hand, which trains them to ignore escalations.
func TestWorkflowPreflightOnlyBlocksWhenAWorkflowActuallyChanged(t *testing.T) {
	t.Run("a restricted run with no workflow change proceeds", func(t *testing.T) {
		f := &forgefake.Forge{}
		v := &fakeVCS{committed: true, workflows: false}
		eng := &Engine{
			Runner: enginefake.New(enginefake.Step{Summary: "changed some Go", Apply: writeFixed}),
			Gate:   gateChecksFile(),
			Forge:  f,
			VCS:    v,
			Cfg: Config{
				WorkDir:                t.TempDir(),
				Branch:                 "loop/1",
				WorkflowRestrictedPush: true,
				SelfLogin:              "acme-code[bot]",
			},
		}

		res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Outcome == OutcomeBlocked {
			t.Fatalf("a change touching no workflow file must not be blocked; reason = %q", res.Reason)
		}
		if !v.pushDone || f.PRCount != 1 {
			t.Fatalf("push=%v PRCount=%d, want the run to reach a pull request", v.pushDone, f.PRCount)
		}
	})

	t.Run("an error deciding is surfaced, not treated as a workflow change", func(t *testing.T) {
		eng := &Engine{
			Runner: enginefake.New(enginefake.Step{Summary: "changed some Go", Apply: writeFixed}),
			Gate:   gateChecksFile(),
			Forge:  &forgefake.Forge{},
			VCS:    &fakeVCS{committed: true, workflowErr: errors.New("git status: exit status 128")},
			Cfg: Config{
				WorkDir:                t.TempDir(),
				Branch:                 "loop/1",
				WorkflowRestrictedPush: true,
			},
		}

		res, err := eng.Run(context.Background(), domain.Issue{Repo: "o/r", Number: 1})
		if err == nil && res.Outcome == OutcomeBlocked && strings.Contains(res.Reason, "workflows") {
			t.Fatal("a failure to decide must not be reported as a workflow-permission problem")
		}
	})
}
