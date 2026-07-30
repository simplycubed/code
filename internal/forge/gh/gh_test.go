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
