package ledger

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// repoWithRemote returns a checkout whose origin is a bare repo, so a push has
// somewhere real to go.
func repoWithRemote(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bare := t.TempDir()
	gitIn(t, bare, "init", "-q", "--bare")
	work := t.TempDir()
	gitIn(t, work, "init", "-q")
	gitIn(t, work, "config", "user.email", "t@example.test")
	gitIn(t, work, "config", "user.name", "t")
	gitIn(t, work, "config", "commit.gpgsign", "false")
	gitIn(t, work, "remote", "add", "origin", bare)
	if err := os.WriteFile(filepath.Join(work, "code.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, work, "add", "-A")
	gitIn(t, work, "commit", "-q", "-m", "seed")
	gitIn(t, work, "push", "-q", "origin", "HEAD")
	return work
}

func TestBranchStoreAppendsAndAccumulates(t *testing.T) {
	repo := repoWithRemote(t)
	s := &BranchStore{RepoDir: repo, AuthorName: "t", AuthorEmail: "t@example.test"}
	day := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	ctx := context.Background()

	if err := s.Append(ctx, `{"run_id":"a"}`, day); err != nil {
		t.Fatalf("first append: %v", err)
	}
	// A second run on the same day extends the file rather than replacing it.
	if err := s.Append(ctx, `{"run_id":"b"}`, day); err != nil {
		t.Fatalf("second append: %v", err)
	}

	got := gitIn(t, repo, "show", "simplycubed/ledger:2026-07-31.jsonl")
	if !strings.Contains(got, `"a"`) || !strings.Contains(got, `"b"`) {
		t.Fatalf("both runs should be recorded, got:\n%s", got)
	}
}

// The ledger must not carry a copy of the source tree: it shares no history
// with the default branch, so it never turns up in a diff or a pull request.
func TestBranchStoreKeepsNoCode(t *testing.T) {
	repo := repoWithRemote(t)
	s := &BranchStore{RepoDir: repo, AuthorName: "t", AuthorEmail: "t@example.test"}
	if err := s.Append(context.Background(), `{"run_id":"a"}`, time.Now()); err != nil {
		t.Fatalf("append: %v", err)
	}
	files := gitIn(t, repo, "ls-tree", "--name-only", "simplycubed/ledger")
	if strings.Contains(files, "code.go") {
		t.Fatalf("the ledger branch must not contain source: %s", files)
	}
	// An orphan branch has no parent, which is what keeps it out of history.
	parents := strings.TrimSpace(gitIn(t, repo, "rev-list", "--parents", "-n", "1", "simplycubed/ledger"))
	if len(strings.Fields(parents)) != 1 {
		t.Fatalf("the first ledger commit should have no parent, got %q", parents)
	}
}

// Losing an audit line must never fail a run that otherwise succeeded.
func TestBranchStoreIgnoresEmptyInput(t *testing.T) {
	s := &BranchStore{RepoDir: t.TempDir()}
	if err := s.Append(context.Background(), "   ", time.Now()); err != nil {
		t.Fatalf("empty input should be a no-op, got: %v", err)
	}
}
