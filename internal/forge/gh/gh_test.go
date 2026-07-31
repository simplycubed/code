package gh

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
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

func TestEnsureLabelsCreatesOnlyMissingLabels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	log := filepath.Join(dir, "args.log")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[{"name":"sc:go"},{"name":"sc:done"}]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", log)

	f := &Forge{Bin: bin}
	created, err := f.EnsureLabels(context.Background(), []string{"sc:go", "sc:queued", "sc:done"})
	if err != nil {
		t.Fatalf("EnsureLabels: %v", err)
	}
	if strings.Join(created, ",") != "sc:queued" {
		t.Fatalf("created = %v, want [sc:queued]", created)
	}
	logged := readLog(t, log)
	if !strings.Contains(logged, "label list --limit 1000 --json name") {
		t.Fatalf("did not list labels first: %s", logged)
	}
	if !strings.Contains(logged, "label create sc:queued") {
		t.Fatalf("missing label not created: %s", logged)
	}
	if strings.Contains(logged, "label create sc:go") || strings.Contains(logged, "label create sc:done") {
		t.Fatalf("existing labels should be left untouched: %s", logged)
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

// stubGHReplying writes a gh stub whose stdout is fixed, so the JSON-parsing
// paths can be exercised without a network.
func stubGHReplying(t *testing.T, stdout string, exit int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gh")
	// Real gh reports failures on stderr, and the 404 detection reads the error
	// text, so the stub has to put it in the same place.
	redirect := ""
	if exit != 0 {
		redirect = " >&2"
	}
	script := "#!/bin/sh\n" +
		"cat" + redirect + " <<'OUT'\n" + stdout + "\nOUT\n" +
		"exit " + strconv.Itoa(exit) + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func TestCanWrite(t *testing.T) {
	ctx := context.Background()
	// Only write and admin may trigger a run. read is the case that matters:
	// a public repository grants it to everyone.
	for perm, want := range map[string]bool{
		"admin": true, "write": true, "read": false, "none": false,
	} {
		f := &Forge{Bin: stubGHReplying(t, `{"permission":"`+perm+`"}`, 0)}
		got, err := f.CanWrite(ctx, "o/r", "someone")
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", perm, err)
		}
		if got != want {
			t.Fatalf("permission %q -> %v, want %v", perm, got, want)
		}
	}
}

// GitHub answers 404 for a login with no access at all. That is a definitive
// no, not a failure to determine, so it must not stop the run with an error.
func TestCanWriteTreats404AsNo(t *testing.T) {
	f := &Forge{Bin: stubGHReplying(t, "gh: Not Found (HTTP 404)", 1)}
	got, err := f.CanWrite(context.Background(), "o/r", "stranger")
	if err != nil {
		t.Fatalf("a 404 should be a plain no, got error: %v", err)
	}
	if got {
		t.Fatal("a 404 must not grant access")
	}
}

func TestWhoami(t *testing.T) {
	f := &Forge{Bin: stubGHReplying(t, `{"data":{"viewer":{"login":"simplycubed-code[bot]"}}}`, 0)}
	got, err := f.Whoami(context.Background())
	if err != nil || got != "simplycubed-code[bot]" {
		t.Fatalf("Whoami() = %q, %v", got, err)
	}
	// An empty login is not a usable identity; reporting it as one would make
	// the self-review filter match everything.
	empty := &Forge{Bin: stubGHReplying(t, `{"data":{"viewer":{"login":""}}}`, 0)}
	if _, err := empty.Whoami(context.Background()); err == nil {
		t.Fatal("an empty login must be an error")
	}
}

// GitHub numbers issues and pull requests from one sequence, so the only way to
// tell them apart is that the issues endpoint carries a pull_request object for
// one and not the other. Both directions matter: a false negative sends the
// fixer at an issue, which is the failure #98 was filed for.
func TestIsPullRequest(t *testing.T) {
	ctx := context.Background()
	for name, tc := range map[string]struct {
		payload string
		want    bool
	}{
		"a pull request carries the pull_request object": {
			payload: `{"number":7,"pull_request":{"url":"https://api.github.com/repos/o/r/pulls/7"}}`,
			want:    true,
		},
		"a plain issue does not": {
			payload: `{"number":7,"title":"something"}`,
			want:    false,
		},
		"an explicit null is not a pull request": {
			payload: `{"number":7,"pull_request":null}`,
			want:    false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := &Forge{Bin: stubGHReplying(t, tc.payload, 0)}
			got, err := f.IsPullRequest(ctx, "o/r", 7)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("IsPullRequest = %v, want %v", got, tc.want)
			}
		})
	}

	t.Run("a failing call is an error, not a guess", func(t *testing.T) {
		f := &Forge{Bin: stubGHReplying(t, "Not Found", 1)}
		if _, err := f.IsPullRequest(ctx, "o/r", 7); err == nil {
			t.Fatal("want an error when gh fails, so the caller does not treat a lookup failure as an issue")
		}
	})

	t.Run("unreadable output is an error", func(t *testing.T) {
		f := &Forge{Bin: stubGHReplying(t, "not json", 0)}
		if _, err := f.IsPullRequest(ctx, "o/r", 7); err == nil {
			t.Fatal("want a parse error")
		}
	})
}
