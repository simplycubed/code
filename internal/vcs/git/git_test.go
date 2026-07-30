package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitCmd(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// repoWithBareRemote sets up a work repo whose "origin" is a local bare repo, so
// push works with no network. Returns the work dir.
func repoWithBareRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := t.TempDir()
	gitCmd(t, bare, "init", "-q", "--bare")

	work := t.TempDir()
	gitCmd(t, work, "init", "-q")
	gitCmd(t, work, "config", "user.email", "t@example.test")
	gitCmd(t, work, "config", "user.name", "t")
	gitCmd(t, work, "config", "commit.gpgsign", "false")
	gitCmd(t, work, "remote", "add", "origin", bare)
	// seed one commit so a branch exists
	if err := os.WriteFile(filepath.Join(work, "seed"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, work, "add", "-A")
	gitCmd(t, work, "commit", "-q", "-m", "seed")
	return work
}

func TestCommitReportsWhetherAnythingChanged(t *testing.T) {
	work := repoWithBareRemote(t)
	g := &Git{}
	ctx := context.Background()

	// Nothing changed since seed: committed=false, no error.
	committed, err := g.Commit(ctx, work, "noop")
	if err != nil || committed {
		t.Fatalf("expected no-op commit, got committed=%v err=%v", committed, err)
	}

	// Only scratch present (no real change): still a no-op, because scratch is
	// excluded. This is what stops an empty run from opening a PR. The .gopath
	// module cache is what a live run actually leaked (empty .lock files).
	if err := os.MkdirAll(filepath.Join(work, ".gocache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".gocache", "junk"), []byte("cache"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(work, ".gopath", "pkg", "mod", "cache", "download"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".gopath", "pkg", "mod", "cache", "download", "v1.0.0.lock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err = g.Commit(ctx, work, "scratch only")
	if err != nil || committed {
		t.Fatalf("scratch-only should be a no-op, got committed=%v err=%v", committed, err)
	}

	// A real change alongside scratch: committed=true, and scratch is not in it.
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err = g.Commit(ctx, work, "add a.txt")
	if err != nil || !committed {
		t.Fatalf("expected a commit, got committed=%v err=%v", committed, err)
	}
	tracked := gitCmd(t, work, "ls-files")
	if !strings.Contains(tracked, "a.txt") {
		t.Fatalf("a.txt should be committed: %s", tracked)
	}
	if strings.Contains(tracked, ".gocache") {
		t.Fatalf(".gocache must never be committed: %s", tracked)
	}
	if strings.Contains(tracked, ".gopath") {
		t.Fatalf(".gopath must never be committed: %s", tracked)
	}
}

func TestPushSendsBranchToRemote(t *testing.T) {
	work := repoWithBareRemote(t)
	g := &Git{}
	ctx := context.Background()

	gitCmd(t, work, "checkout", "-q", "-b", "loop/1")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Commit(ctx, work, "change on branch"); err != nil {
		t.Fatal(err)
	}
	if err := g.Push(ctx, work, "loop/1"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	// The bare remote should now have the branch.
	remote := gitCmd(t, work, "ls-remote", "--heads", "origin", "loop/1")
	if !strings.Contains(remote, "loop/1") {
		t.Fatalf("branch not on remote: %q", remote)
	}
}

// A GitHub Actions runner has no git identity, so a commit there fails with
// "Author identity unknown" after the change is made and the gate has passed:
// the most expensive possible moment to discover it. Supplying the identity per
// command avoids that without mutating the machine's global config.
func TestCommitUsesTheConfiguredIdentity(t *testing.T) {
	work := repoWithBareRemote(t)
	// Remove the identity this repo was seeded with, reproducing a bare runner.
	gitCmd(t, work, "config", "--unset", "user.email")
	gitCmd(t, work, "config", "--unset", "user.name")

	g := &Git{AuthorName: "simplycubed-code[bot]", AuthorEmail: "simplycubed-code@users.noreply.github.com"}
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err := g.Commit(context.Background(), work, "add a.txt")
	if err != nil || !committed {
		t.Fatalf("commit with an explicit identity should succeed: committed=%v err=%v", committed, err)
	}
	author := gitCmd(t, work, "log", "-1", "--format=%an <%ae>")
	if !strings.Contains(author, "simplycubed-code[bot]") {
		t.Fatalf("author = %q, want the configured identity", author)
	}
}

func TestCommitWithoutAnIdentityStillWorksWhenGitHasOne(t *testing.T) {
	work := repoWithBareRemote(t) // seeded with a local identity
	g := &Git{}
	if err := os.WriteFile(filepath.Join(work, "b.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if committed, err := g.Commit(context.Background(), work, "add b.txt"); err != nil || !committed {
		t.Fatalf("committed=%v err=%v", committed, err)
	}
}

func TestTouchesWorkflowReportsOnlyWorkflowChanges(t *testing.T) {
	work := repoWithBareRemote(t)
	g := &Git{}
	ctx := context.Background()

	touched, err := g.TouchesWorkflow(ctx, work)
	if err != nil || touched {
		t.Fatalf("clean repo should report no workflow changes: touched=%v err=%v", touched, err)
	}

	if err := os.MkdirAll(filepath.Join(work, ".github", "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".github", "workflows", "check.yml"), []byte("name: check\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	touched, err = g.TouchesWorkflow(ctx, work)
	if err != nil || !touched {
		t.Fatalf("workflow edit should be reported: touched=%v err=%v", touched, err)
	}

	if _, err := g.Commit(ctx, work, "add workflow"); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	touched, err = g.TouchesWorkflow(ctx, work)
	if err != nil || touched {
		t.Fatalf("committed workflow should leave a clean tree: touched=%v err=%v", touched, err)
	}

	if err := os.WriteFile(filepath.Join(work, "plain.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	touched, err = g.TouchesWorkflow(ctx, work)
	if err != nil {
		t.Fatalf("TouchesWorkflow: %v", err)
	}
	if touched {
		t.Fatal("non-workflow edits must not trip workflow detection")
	}
}

// A dry run may commit, because the worktree is thrown away and the commit is
// what makes the change inspectable. Pushing is the first step that changes
// someone's repository, so it must not happen.
func TestDryRunCommitsButDoesNotPush(t *testing.T) {
	work := repoWithBareRemote(t)
	g := &Git{DryRun: true}
	ctx := context.Background()
	gitCmd(t, work, "checkout", "-q", "-b", "sc/1")
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err := g.Commit(ctx, work, "change")
	if err != nil || !committed {
		t.Fatalf("a dry run should still commit: committed=%v err=%v", committed, err)
	}
	if err := g.Push(ctx, work, "sc/1"); err != nil {
		t.Fatalf("dry-run push should be a no-op, got: %v", err)
	}
	// The remote must not have the branch.
	out := gitCmd(t, work, "ls-remote", "--heads", "origin", "sc/1")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("a dry run must not push, but the remote has the branch: %s", out)
	}
}
