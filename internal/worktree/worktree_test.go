package worktree

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initRepo makes a temp git repo with one commit and returns its path.
func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@example.test")
	run("config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestAddListRemove(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := initRepo(t)
	m := &Manager{RepoDir: repo, BaseDir: t.TempDir()}
	ctx := context.Background()

	path, err := m.Add(ctx, "loop/issue-1", "HEAD")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(filepath.Join(path, "f.txt")); err != nil {
		t.Fatalf("worktree missing committed file: %v", err)
	}

	list, err := m.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, p := range list {
		if strings.HasSuffix(p, "loop-issue-1") {
			found = true
		}
	}
	if !found {
		t.Fatalf("new worktree not in list: %v", list)
	}

	// Add again is idempotent (branch reset, no error).
	if _, err := m.Add(ctx, "loop/issue-1", "HEAD"); err != nil {
		t.Fatalf("re-Add should be idempotent: %v", err)
	}

	if err := m.Remove(ctx, path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after Remove")
	}
}
