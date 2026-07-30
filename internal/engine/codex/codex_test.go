package codex

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
)

// writeFakeCodex writes a stub that mimics the parts of `codex exec` the adapter
// relies on: it finds the -o <file> argument and writes a canned final message
// there, then exits. This keeps the test hermetic, with no real codex or network.
func writeFakeCodex(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake codex stub is a POSIX shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fakecodex")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const fakeOK = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$out" ] && printf 'DONE: fixed it' > "$out"
echo "fake codex ran"
exit 0
`

const fakeFail = `#!/bin/sh
echo "fake codex error" 1>&2
exit 7
`

func TestRunReturnsFinalMessage(t *testing.T) {
	bin := writeFakeCodex(t, fakeOK)
	r := New(t.TempDir())
	r.Bin = bin
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role:    domain.RoleImplementer,
		WorkDir: t.TempDir(),
		Prompt:  "fix the failing gate",
		Model:   "gpt-5.4",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary != "DONE: fixed it" {
		t.Fatalf("summary = %q want the final message", res.Summary)
	}
}

func TestRunSurfacesCodexFailure(t *testing.T) {
	bin := writeFakeCodex(t, fakeFail)
	r := New(t.TempDir())
	r.Bin = bin
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role:    domain.RoleImplementer,
		WorkDir: t.TempDir(),
		Prompt:  "x",
	})
	if err == nil {
		t.Fatal("expected an error when codex exits non-zero")
	}
	if res.Err == nil {
		t.Fatal("RunResult.Err should carry the failure")
	}
}

// The adapter must point GOCACHE and GOPATH at workspace-local paths so a Go
// toolchain can write its build and module caches inside the sandbox (the S3-B
// lesson, and the live run that saw the agent invent its own GOPATH and commit
// it). This stub writes the values it saw into the final-message file so the
// test can inspect them.
const fakeEchoGoEnv = `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
[ -n "$out" ] && printf '%s\n%s' "$GOCACHE" "$GOPATH" > "$out"
exit 0
`

func TestRunSetsWorkspaceLocalGoCaches(t *testing.T) {
	bin := writeFakeCodex(t, fakeEchoGoEnv)
	work := t.TempDir()
	r := New(t.TempDir())
	r.Bin = bin
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role:    domain.RoleImplementer,
		WorkDir: work,
		Prompt:  "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(res.Summary, "\n")
	if len(lines) != 2 {
		t.Fatalf("summary = %q, want GOCACHE and GOPATH on two lines", res.Summary)
	}
	if !strings.HasPrefix(lines[0], work) || !strings.HasSuffix(lines[0], ".gocache") {
		t.Fatalf("GOCACHE = %q, want a .gocache dir under the workdir %q", lines[0], work)
	}
	if !strings.HasPrefix(lines[1], work) || !strings.HasSuffix(lines[1], ".gopath") {
		t.Fatalf("GOPATH = %q, want a .gopath dir under the workdir %q", lines[1], work)
	}
}

func TestRunRequiresCodexHome(t *testing.T) {
	r := &Runner{} // no CodexHome
	_, err := r.Run(context.Background(), domain.RunRequest{Role: domain.RoleImplementer, WorkDir: t.TempDir()})
	if err == nil {
		t.Fatal("expected an error when CodexHome is unset")
	}
}
