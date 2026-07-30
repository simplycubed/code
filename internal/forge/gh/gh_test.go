package gh

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubGH writes a fake `gh` that records its argument line to $GH_STUB_LOG and
// prints a PR URL for `pr create`. It returns the stub path and the log path.
func stubGH(t *testing.T) (bin, log string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin = filepath.Join(dir, "gh")
	log = filepath.Join(dir, "args.log")
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> \"$GH_STUB_LOG\"\n" +
		"if [ \"$1 $2\" = \"pr create\" ]; then echo \"https://github.com/o/r/pull/123\"; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", log)
	return bin, log
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading stub log: %v", err)
	}
	return string(b)
}

func TestOpenPRReturnsURL(t *testing.T) {
	bin, log := stubGH(t)
	f := &Forge{Bin: bin}
	url, err := f.OpenPR(context.Background(), "o/r", "loop/1", "Closes #1", "body")
	if err != nil {
		t.Fatalf("OpenPR: %v", err)
	}
	if url != "https://github.com/o/r/pull/123" {
		t.Fatalf("url = %q", url)
	}
	logged := readLog(t, log)
	if !strings.Contains(logged, "pr create") || !strings.Contains(logged, "--head loop/1") {
		t.Fatalf("gh not invoked as expected: %s", logged)
	}
}

// The invariant that only this code enforces: setting a state removes every
// other state label, so exactly one remains.
func TestSetStateRemovesPriorStateLabels(t *testing.T) {
	bin, log := stubGH(t)
	states := []string{"sc:go", "sc:queued", "sc:working", "sc:review", "sc:blocked", "sc:done"}
	f := &Forge{Bin: bin, StateLabels: states}

	if err := f.SetState(context.Background(), "o/r", 7, "sc:working"); err != nil {
		t.Fatalf("SetState: %v", err)
	}
	logged := readLog(t, log)

	if !strings.Contains(logged, "--add-label sc:working") {
		t.Fatalf("did not add the new state label: %s", logged)
	}
	if strings.Contains(logged, "--remove-label sc:working") {
		t.Fatalf("must not remove the label it is setting: %s", logged)
	}
	for _, other := range []string{"sc:go", "sc:queued", "sc:review", "sc:blocked", "sc:done"} {
		if !strings.Contains(logged, "--remove-label "+other) {
			t.Fatalf("did not remove prior state label %s: %s", other, logged)
		}
	}
}

func TestCommentPostsBody(t *testing.T) {
	bin, log := stubGH(t)
	f := &Forge{Bin: bin}
	if err := f.Comment(context.Background(), "o/r", 7, "blocked: stalled"); err != nil {
		t.Fatalf("Comment: %v", err)
	}
	logged := readLog(t, log)
	if !strings.Contains(logged, "issue comment 7") || !strings.Contains(logged, "blocked: stalled") {
		t.Fatalf("comment not posted as expected: %s", logged)
	}
}

func TestCommentPRUsesPRSurface(t *testing.T) {
	bin, log := stubGH(t)
	f := &Forge{Bin: bin}
	if err := f.CommentPR(context.Background(), "o/r", 42, "ready for another look"); err != nil {
		t.Fatalf("CommentPR: %v", err)
	}
	logged := readLog(t, log)
	if !strings.Contains(logged, "pr comment 42") || !strings.Contains(logged, "ready for another look") {
		t.Fatalf("PR comment not posted as expected: %s", logged)
	}
}

// stubGHFeedback writes a gh stub that returns canned PR metadata, reviews, and
// review comments, so the freshness and self filters in Feedback can be tested
// without GitHub. The head SHA is "HEAD1"; anything at "OLD0" is stale.
func stubGHFeedback(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	script := `#!/bin/sh
if [ "$1" = "pr" ] && [ "$2" = "view" ]; then
  echo '{"headRefName":"pr-branch","headRefOid":"HEAD1","title":"Closes #7: add feature"}'
  exit 0
fi
if [ "$1" = "api" ]; then
  # Emulate gh api --paginate --slurp: an array whose elements are per-page arrays.
  case "$2" in
    *reviews*) echo '[[{"user":{"login":"human"},"body":"please rename X","state":"CHANGES_REQUESTED","commit_id":"HEAD1"},{"user":{"login":"human"},"body":"stale note","state":"CHANGES_REQUESTED","commit_id":"OLD0"},{"user":{"login":"simplycubed-code[bot]"},"body":"i addressed it","state":"COMMENTED","commit_id":"HEAD1"},{"user":{"login":"human"},"body":"","state":"COMMENTED","commit_id":"HEAD1"}]]' ;;
    *comments*) echo '[[{"user":{"login":"human"},"body":"fix this line","path":"main.go","line":10,"original_line":9,"commit_id":"HEAD1"},{"user":{"login":"human"},"body":"old inline","path":"main.go","line":5,"commit_id":"OLD0"}]]' ;;
  esac
  exit 0
fi
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

// Feedback must return only feedback left against the current head and not
// authored by the agent itself. This is the filter that keeps the fix loop from
// re-addressing old or self-authored feedback forever (the dead review/fix cycle
// looper hit from a different direction).
func TestFeedbackFiltersToHeadAndExcludesSelf(t *testing.T) {
	f := &Forge{Bin: stubGHFeedback(t), Self: "simplycubed-code[bot]"}
	fb, err := f.Feedback(context.Background(), "o/r", 42)
	if err != nil {
		t.Fatalf("Feedback: %v", err)
	}
	if fb.Branch != "pr-branch" || fb.HeadSHA != "HEAD1" {
		t.Fatalf("meta wrong: branch=%q head=%q", fb.Branch, fb.HeadSHA)
	}
	if !strings.Contains(fb.Title, "Closes #7") {
		t.Fatalf("title = %q", fb.Title)
	}
	// Exactly two survive: the human review at head, and the human inline comment
	// at head. Stale (OLD0), self-authored, and empty-body feedback are dropped.
	if len(fb.Notes) != 2 {
		t.Fatalf("got %d notes want 2: %+v", len(fb.Notes), fb.Notes)
	}
	var sawReview, sawInline bool
	for _, n := range fb.Notes {
		if n.Author == "simplycubed-code[bot]" {
			t.Fatalf("self-authored feedback leaked in: %+v", n)
		}
		if strings.Contains(n.Body, "stale") || strings.Contains(n.Body, "old inline") {
			t.Fatalf("stale feedback leaked in: %+v", n)
		}
		if n.File == "" && n.Body == "please rename X" {
			sawReview = true
		}
		if n.File == "main.go" && n.Line == 10 && n.Body == "fix this line" {
			sawInline = true
		}
	}
	if !sawReview || !sawInline {
		t.Fatalf("expected the head review and inline comment; notes=%+v", fb.Notes)
	}
}
