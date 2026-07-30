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

	// A real change: committed=true.
	if err := os.WriteFile(filepath.Join(work, "a.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	committed, err = g.Commit(ctx, work, "add a.txt")
	if err != nil || !committed {
		t.Fatalf("expected a commit, got committed=%v err=%v", committed, err)
	}
	if log := gitCmd(t, work, "log", "--oneline", "-1"); !strings.Contains(log, "add a.txt") {
		t.Fatalf("commit not recorded: %s", log)
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
