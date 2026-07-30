package claude

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
)

// writeFakeClaude installs a stub on PATH so the adapter can be exercised with
// no model, no network, and no spend.
func writeFakeClaude(t *testing.T, script string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("stub is a POSIX shell script")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "claude")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

const echoArgs = `#!/bin/sh
echo "$@"
exit 0
`

func TestRunPassesThePromptAndModel(t *testing.T) {
	r := New()
	r.Bin = writeFakeClaude(t, echoArgs)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(),
		Prompt: "do the thing", Model: "claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"-p", "do the thing", "--model", "claude-sonnet-4-5"} {
		if !strings.Contains(res.Summary, want) {
			t.Fatalf("args missing %q: %s", want, res.Summary)
		}
	}
}

// A headless run has nobody to answer a permission prompt, but an adopter must
// be able to turn that off.
// The vendor's danger flag is off unless an operator asks for it. Defaulting it
// on would have the product disable a safety control on the operator's behalf.
func TestRunDoesNotSkipPermissionsByDefault(t *testing.T) {
	r := New()
	r.Bin = writeFakeClaude(t, echoArgs)
	res, _ := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if strings.Contains(res.Summary, "--dangerously-skip-permissions") {
		t.Fatalf("the danger flag must be off by default: %s", res.Summary)
	}
	r.SkipPermissions = true
	res, _ = r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if !strings.Contains(res.Summary, "--dangerously-skip-permissions") {
		t.Fatalf("opting in should pass the flag: %s", res.Summary)
	}
}

// The Go caches must land inside the workspace, where the VCS scratch rules
// keep them out of a commit. Without this the toolchain hits a read-only cache
// and the agent works around the environment instead of the bug.
const echoGoEnv = `#!/bin/sh
printf '%s\n%s\n' "$GOCACHE" "$GOPATH"
exit 0
`

func TestRunSetsWorkspaceLocalGoCaches(t *testing.T) {
	work := t.TempDir()
	r := New()
	r.Bin = writeFakeClaude(t, echoGoEnv)
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: work, Prompt: "x",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	lines := strings.Split(res.Summary, "\n")
	if len(lines) != 2 {
		t.Fatalf("summary = %q, want GOCACHE and GOPATH", res.Summary)
	}
	if !strings.HasPrefix(lines[0], work) || !strings.HasSuffix(lines[0], ".gocache") {
		t.Fatalf("GOCACHE = %q, want a .gocache under %q", lines[0], work)
	}
	if !strings.HasPrefix(lines[1], work) || !strings.HasSuffix(lines[1], ".gopath") {
		t.Fatalf("GOPATH = %q, want a .gopath under %q", lines[1], work)
	}
}

// A CLI failure is an engine error, which is distinct from a gate failure: the
// loop escalates on the first and retries on the second.
func TestRunReportsAFailureAsAnEngineError(t *testing.T) {
	r := New()
	r.Bin = writeFakeClaude(t, "#!/bin/sh\necho boom >&2\nexit 3\n")
	res, err := r.Run(context.Background(), domain.RunRequest{
		Role: domain.RoleImplementer, WorkDir: t.TempDir(), Prompt: "x",
	})
	if err == nil || res.Err == nil {
		t.Fatal("a non-zero exit must surface as an engine error")
	}
	if !strings.Contains(res.Summary, "boom") {
		t.Fatalf("the CLI's own output should be kept: %q", res.Summary)
	}
}

func TestRunRequiresAWorkDir(t *testing.T) {
	if _, err := New().Run(context.Background(), domain.RunRequest{Role: domain.RoleImplementer}); err == nil {
		t.Fatal("expected an error with no WorkDir")
	}
}
