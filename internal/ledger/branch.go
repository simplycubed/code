package ledger

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// BranchStore appends ledger lines to an orphan branch, so a run's audit trail
// outlives the runner it happened on. The branch carries no code and shares no
// history with the default branch, so it never appears in a diff, a pull
// request, or a clone that does not ask for it.
//
// Appending is best-effort by contract: losing an audit line must never fail a
// run that otherwise succeeded. Errors are returned for logging, not for
// control flow.
type BranchStore struct {
	RepoDir string // a git checkout with a remote
	Branch  string // defaults to "simplycubed/ledger"
	Remote  string // defaults to "origin"
	// AuthorName and AuthorEmail identify the committer, for the same reason
	// the VCS layer needs them: a runner has no git identity.
	AuthorName  string
	AuthorEmail string
}

func (s *BranchStore) branch() string {
	if s.Branch != "" {
		return s.Branch
	}
	return "simplycubed/ledger"
}

func (s *BranchStore) remote() string {
	if s.Remote != "" {
		return s.Remote
	}
	return "origin"
}

func (s *BranchStore) git(ctx context.Context, dir string, args ...string) (string, error) {
	full := args
	if s.AuthorName != "" {
		full = append([]string{"-c", "user.name=" + s.AuthorName}, full...)
	}
	if s.AuthorEmail != "" {
		full = append([]string{"-c", "user.email=" + s.AuthorEmail}, full...)
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, full...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// Append adds lines to the ledger branch and pushes it. The file is named per
// day, so the branch stays browsable rather than becoming one enormous blob.
func (s *BranchStore) Append(ctx context.Context, lines string, day time.Time) error {
	if strings.TrimSpace(lines) == "" {
		return nil
	}
	work, err := os.MkdirTemp("", "sc-ledger-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	branch := s.branch()
	// Fetch the branch if it exists; otherwise start an orphan with no history,
	// so the ledger never carries a copy of the source tree.
	if _, err := s.git(ctx, s.RepoDir, "fetch", s.remote(), branch); err == nil {
		if out, err := s.git(ctx, s.RepoDir, "worktree", "add", "--detach", work, "FETCH_HEAD"); err != nil {
			return fmt.Errorf("ledger worktree: %w: %s", err, out)
		}
		if _, err := s.git(ctx, work, "checkout", "-B", branch); err != nil {
			return err
		}
	} else {
		if out, err := s.git(ctx, s.RepoDir, "worktree", "add", "--detach", work); err != nil {
			return fmt.Errorf("ledger worktree: %w: %s", err, out)
		}
		if _, err := s.git(ctx, work, "checkout", "--orphan", branch); err != nil {
			return err
		}
		if _, err := s.git(ctx, work, "rm", "-rf", "--cached", "."); err != nil {
			return err
		}
		entries, _ := os.ReadDir(work)
		for _, e := range entries {
			if e.Name() != ".git" {
				_ = os.RemoveAll(filepath.Join(work, e.Name()))
			}
		}
	}
	defer func() { _, _ = s.git(ctx, s.RepoDir, "worktree", "remove", "--force", work) }()

	name := day.UTC().Format("2006-01-02") + ".jsonl"
	path := filepath.Join(work, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(lines, "\n") {
		lines += "\n"
	}
	if _, err := f.WriteString(lines); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	if _, err := s.git(ctx, work, "add", name); err != nil {
		return err
	}
	if _, err := s.git(ctx, work, "commit", "-m", "ledger: "+name); err != nil {
		return err
	}
	if _, err := s.git(ctx, work, "push", s.remote(), branch); err != nil {
		return err
	}
	return nil
}
