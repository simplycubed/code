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
	vcsgit "github.com/simplycubed/code/internal/vcs/git"
	"github.com/simplycubed/code/internal/worktree"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// setupRemoteRepo builds a bare remote with a main branch and a pushed pull-
// request branch, plus a working clone. It returns the clone dir, the pull-
// request branch name, and that branch's current head SHA on the remote.
func setupRemoteRepo(t *testing.T) (repoDir, prBranch, headSHA string) {
	t.Helper()
	remote := t.TempDir()
	git(t, remote, "init", "--bare", "-q")

	repoDir = t.TempDir()
	git(t, repoDir, "clone", "-q", remote, ".")
	git(t, repoDir, "config", "user.email", "t@example.test")
	git(t, repoDir, "config", "user.name", "t")
	git(t, repoDir, "config", "commit.gpgsign", "false")

	// main
	if err := os.WriteFile(filepath.Join(repoDir, "seed"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repoDir, "add", "-A")
	git(t, repoDir, "commit", "-q", "-m", "seed")
	git(t, repoDir, "branch", "-M", "main")
	git(t, repoDir, "push", "-q", "origin", "main")

	// pull-request branch with a commit, pushed
	prBranch = "pr-branch"
	git(t, repoDir, "checkout", "-q", "-b", prBranch)
	if err := os.WriteFile(filepath.Join(repoDir, "feature"), []byte("wip"), 0o644); err != nil {
		t.Fatal(err)
	}
	git(t, repoDir, "add", "-A")
	git(t, repoDir, "commit", "-q", "-m", "wip")
	git(t, repoDir, "push", "-q", "origin", prBranch)
	headSHA = git(t, repoDir, "rev-parse", "HEAD")

	// leave the main checkout on main so the worktree can take the PR branch
	git(t, repoDir, "checkout", "-q", "main")
	return repoDir, prBranch, headSHA
}

// End-to-end fix flow against a real repo and bare remote, using the real git
// VCS: it proves the worktree is synced to the pull request head, the fixer's
// change is committed and pushed to the existing branch (not a new one), and the
// commit carries the attribution trailer. This is the path that silently breaks
// if the worktree is reused stale, so it uses real git rather than a fake VCS.
func TestAddressPRPushesFixToExistingBranch(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repoDir, prBranch, headSHA := setupRemoteRepo(t)
	remoteBefore := git(t, repoDir, "rev-parse", "origin/"+prBranch)

	ff := &forgefake.Forge{Feedbacks: []domain.ReviewFeedback{{
		PR:      42,
		Branch:  prBranch,
		HeadSHA: headSHA,
		Title:   "Closes #7: add the feature",
		Notes:   []domain.ReviewNote{{Author: "human", File: "feature", Line: 1, Body: "rename this"}},
	}}}

	// The fixer writes the gate's marker into the worktree.
	eng := &recordingEngine{}
	d := app.Deps{
		Runner:    eng,
		Forge:     ff,
		VCS:       &vcsgit.Git{},
		Worktrees: &worktree.Manager{RepoDir: repoDir, BaseDir: t.TempDir()},
	}
	cfg := &config.Config{LabelPrefix: "sc", Gate: "test -f fixed", Attribution: true}

	res, err := app.AddressPR(context.Background(), d, cfg, "o/r", 42)
	if err != nil {
		t.Fatalf("AddressPR: %v", err)
	}
	if res.Outcome != loop.OutcomeChangesPushed {
		t.Fatalf("outcome = %s want changes_pushed (reason %s)", res.Outcome, res.Reason)
	}
	// The fixer got a prompt carrying the bounds and the feedback.
	if !strings.Contains(eng.lastPrompt, "rename this") || !strings.Contains(eng.lastPrompt, "Never modify the gate") {
		t.Fatalf("fixer prompt missing feedback or bounds:\n%s", eng.lastPrompt)
	}
	// The remote PR branch advanced by exactly the fixer's commit.
	remoteAfter := git(t, repoDir, "rev-parse", "origin/"+prBranch)
	if remoteAfter == remoteBefore {
		t.Fatal("remote PR branch did not advance; the fix was not pushed")
	}
	msg := git(t, repoDir, "log", "-1", "--format=%B", "origin/"+prBranch)
	if !strings.Contains(msg, "Address review feedback") {
		t.Fatalf("pushed commit message = %q", msg)
	}
	if !strings.Contains(msg, "Co-Authored-By: SimplyCubed Code") {
		t.Fatalf("expected attribution trailer in pushed commit: %q", msg)
	}
	// No new PR opened; the PR was commented and returned to review.
	if ff.PRCount != 0 {
		t.Fatalf("fix flow opened %d PRs; must be 0", ff.PRCount)
	}
	if len(ff.PRComments) != 1 {
		t.Fatalf("expected one ack comment on the PR, got %v", ff.PRComments)
	}
}

// No new feedback is a clean no-op: no worktree work, no push, a distinct
// outcome that is neither blocked nor an error.
func TestAddressPRNoFeedbackIsCleanNoOp(t *testing.T) {
	ff := &forgefake.Forge{} // no scripted feedback => empty
	d := app.Deps{
		Runner:    &recordingEngine{},
		Forge:     ff,
		VCS:       &vcsgit.Git{},
		Worktrees: &worktree.Manager{RepoDir: t.TempDir(), BaseDir: t.TempDir()},
	}
	cfg := &config.Config{LabelPrefix: "sc", Gate: "test -f fixed", Attribution: true}

	res, err := app.AddressPR(context.Background(), d, cfg, "o/r", 42)
	if err != nil {
		t.Fatalf("AddressPR: %v", err)
	}
	if res.Outcome != loop.OutcomeNoFeedback {
		t.Fatalf("outcome = %s want no_feedback", res.Outcome)
	}
	if len(ff.PRComments) != 0 || len(ff.States) != 0 {
		t.Fatalf("no-op should touch nothing: comments=%v states=%v", ff.PRComments, ff.States)
	}
}
