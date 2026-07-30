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
	run("config", "commit.gpgsign", "false") // don't depend on a host GPG agent
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

// cloneOf makes a clone of src and, when stripHead is true, deletes the
// origin/HEAD symbolic ref. That reproduces a GitHub Actions checkout, which
// never creates it: the "origin/HEAD" default base is then an invalid
// reference and every run failed at worktree creation (issue #57).
func cloneOf(t *testing.T, src string, stripHead bool) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "clone")
	run := func(wd string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = wd
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
	}
	run("", "clone", "-q", src, dir)
	if stripHead {
		cmd := exec.Command("git", "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
		cmd.Dir = dir
		_ = cmd.Run() // absent already is fine; that is the state we want
	}
	return dir
}

func TestAddResolvesBaseWhenOriginHeadIsMissing(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := cloneOf(t, initRepo(t), true)
	m := &Manager{RepoDir: repo, BaseDir: t.TempDir()}
	ctx := context.Background()

	// The literal ref must really be gone, or this test proves nothing.
	if m.refExists(ctx, "origin/HEAD") {
		t.Fatal("origin/HEAD should be absent for this test to be meaningful")
	}

	path, err := m.Add(ctx, "sc/57", "origin/HEAD")
	if err != nil {
		t.Fatalf("Add with a missing origin/HEAD must resolve a base, got: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree path not created: %v", err)
	}
}

func TestResolveBaseKeepsAnExistingRefAndReportsAMissingOne(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	m := &Manager{RepoDir: initRepo(t), BaseDir: t.TempDir()}
	ctx := context.Background()

	got, err := m.ResolveBase(ctx, "HEAD")
	if err != nil || got != "HEAD" {
		t.Fatalf("an existing ref must be returned unchanged: got %q err %v", got, err)
	}
	// A named base that does not exist is a real error, not something to guess at.
	if _, err := m.ResolveBase(ctx, "origin/nope"); err == nil {
		t.Fatal("a missing named base must error rather than fall back")
	}
}

// gitIn runs a git command in dir and reports whether it succeeded.
func gitIn(t *testing.T, dir string, args ...string) bool {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run() == nil
}

// unreachableClone reproduces the harder half of issue #57: origin/HEAD is
// absent AND the remote cannot be asked for it (no network on the runner, a
// deleted remote, a credential that has expired). Only the local remote-tracking
// refs are left to go on.
func unreachableClone(t *testing.T) string {
	t.Helper()
	src := initRepo(t)
	if !gitIn(t, src, "branch", "-M", "main") {
		t.Fatal("could not name the source branch main")
	}
	dir := cloneOf(t, src, true)
	if !gitIn(t, dir, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git")) {
		t.Fatal("could not point origin at a missing path")
	}
	return dir
}

func TestResolveBaseFallsBackToOriginMainWhenTheRemoteIsUnreachable(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := unreachableClone(t)
	m := &Manager{RepoDir: dir, BaseDir: t.TempDir()}
	ctx := context.Background()

	got, err := m.ResolveBase(ctx, "origin/HEAD")
	if err != nil {
		t.Fatalf("expected a fallback to the conventional branch, got: %v", err)
	}
	if got != "origin/main" {
		t.Fatalf("base = %q, want origin/main", got)
	}
}

func TestResolveBaseErrorsWhenNoDefaultBranchCanBeFound(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := unreachableClone(t)
	// Remove the last thing it could fall back to.
	if !gitIn(t, dir, "update-ref", "-d", "refs/remotes/origin/main") {
		t.Fatal("could not remove the remote-tracking branch")
	}
	m := &Manager{RepoDir: dir, BaseDir: t.TempDir()}

	_, err := m.ResolveBase(context.Background(), "origin/HEAD")
	if err == nil {
		t.Fatal("expected an error when no base can be resolved")
	}
	// The message has to say what it tried, or the operator is left guessing.
	if !strings.Contains(err.Error(), "origin/HEAD") {
		t.Fatalf("error should name the base it could not resolve: %v", err)
	}
}
