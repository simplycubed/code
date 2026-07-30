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
