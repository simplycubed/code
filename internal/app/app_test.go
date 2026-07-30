package app_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/app"
	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/domain"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
	"github.com/simplycubed/code/internal/loop"
	"github.com/simplycubed/code/internal/worktree"
)

func TestParseIssueRef(t *testing.T) {
	iss, err := app.ParseIssueRef("simplycubed/code#42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss.Repo != "simplycubed/code" || iss.Number != 42 {
		t.Fatalf("parsed %+v", iss)
	}
	for _, bad := range []string{"", "not-a-ref", "owner/repo", "owner/repo#", "owner#3"} {
		if _, err := app.ParseIssueRef(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestStateLabels(t *testing.T) {
	got := strings.Join(app.StateLabels("sc"), " ")
	for _, want := range []string{"sc:go", "sc:working", "sc:done"} {
		if !strings.Contains(got, want) {
			t.Fatalf("StateLabels missing %q: %s", want, got)
		}
	}
}

// recordingEngine captures the prompt it was given and writes the "fixed" marker
// so the real gate ("test -f fixed") passes.
type recordingEngine struct{ lastPrompt string }

func (r *recordingEngine) Run(_ context.Context, req domain.RunRequest) (domain.RunResult, error) {
	r.lastPrompt = req.Prompt
	return domain.RunResult{Role: req.Role}, os.WriteFile(filepath.Join(req.WorkDir, "fixed"), []byte("x"), 0o644)
}

func gitInit(t *testing.T, dir string) {
	t.Helper()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.test"},
		{"config", "user.name", "t"},
		{"config", "commit.gpgsign", "false"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "seed"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "-m", "seed"}} {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
}

// End-to-end app composition: a real worktree over a temp repo, the real gate
// runner, fakes for the model and forge. Proves the app onboards a worktree,
// wires the bounded role prompt, and drives the loop to a PR.
func TestRunOnboardsWorktreeAndDrivesLoop(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	gitInit(t, repo)

	eng := &recordingEngine{}
	ff := &forgefake.Forge{}
	baseDir := t.TempDir()
	d := app.Deps{
		Runner:    eng,
		Forge:     ff,
		VCS:       nil, // exercised in the loop package; here we test the app glue
		Worktrees: &worktree.Manager{RepoDir: repo, BaseDir: baseDir},
	}
	cfg := &config.Config{LabelPrefix: "sc", Gate: "test -f fixed"}

	res, err := app.Run(context.Background(), d, cfg, domain.Issue{Repo: "o/r", Number: 12, Title: "t", Body: "b"}, "HEAD")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Outcome != loop.OutcomePROpened {
		t.Fatalf("outcome = %s want pr_opened (reason %s)", res.Outcome, res.Reason)
	}
	if ff.PRCount != 1 {
		t.Fatalf("PRCount = %d want 1", ff.PRCount)
	}
	// The prompt the engine received must carry the role bounds (roles wiring).
	if !strings.Contains(eng.lastPrompt, "Never modify the gate") {
		t.Fatalf("engine prompt did not include the role bounds:\n%s", eng.lastPrompt)
	}
	_ = baseDir // worktree lives here; scratch exclusion is covered in the git VCS test
}
