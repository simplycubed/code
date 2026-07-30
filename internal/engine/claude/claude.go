// Package claude implements engine.Runner by shelling out to Claude Code in
// headless mode (`claude -p`). It is the second engine adapter, and exists to
// prove the Runner seam is real: the loop, the roles, and the gate are unchanged
// by which model writes the code.
//
// It follows the same two lessons as the codex adapter (see docs/decisions/0003):
//
//   - The environment is pre-configured so the agent fixes the bug rather than
//     the environment. Workspace-local Go caches are set here for the same
//     reason they are set there: without them the toolchain hits a read-only
//     cache and the agent is tempted to work around it.
//   - This adapter runs the turn and nothing else. It does not police what the
//     agent changed; that is the gate's job and the PR guard's job.
package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/simplycubed/code/internal/domain"
)

// Runner runs one role turn via `claude -p`.
type Runner struct {
	// Bin is the claude binary. Defaults to "claude" (resolved on PATH).
	Bin string
	// Model is the model to request. Empty uses the CLI's own default, so an
	// adopter who has configured one is not overridden.
	Model string
	// AllowedTools, when set, is passed through as --allowedTools. Empty leaves
	// the CLI's default, which is what a headless run normally wants.
	AllowedTools string
	// ExtraEnv is appended to the child environment, for pre-configuring the
	// workspace. See the package doc.
	ExtraEnv []string
	// SkipPermissions passes --dangerously-skip-permissions. It is OFF by
	// default and has to be opted into.
	//
	// The vendor named the flag "dangerous" on purpose. A product whose whole
	// claim is that a human stays in the loop should not turn that off for the
	// operator as a convenience, and a headless run being awkward is not a
	// reason to: it is a reason to configure the tool properly.
	SkipPermissions bool
}

// New returns a Runner with the defaults applied.
func New() *Runner { return &Runner{} }

func (r *Runner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "claude"
}

// Run executes one turn. A non-nil error means the CLI itself failed; it does
// not mean the gate failed, which is a separate signal the loop grades.
func (r *Runner) Run(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if req.WorkDir == "" {
		err := errors.New("claude: WorkDir is required")
		return domain.RunResult{Role: req.Role, Err: err}, err
	}

	args := []string{"-p", req.Prompt, "--output-format", "text"}
	if r.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	model := req.Model
	if model == "" {
		model = r.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if r.AllowedTools != "" {
		args = append(args, "--allowedTools", r.AllowedTools)
	}

	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = req.WorkDir
	cmd.Env = r.childEnv(req.WorkDir)
	out, runErr := cmd.CombinedOutput()

	res := domain.RunResult{Role: req.Role, Summary: tail(strings.TrimSpace(string(out)), 40)}
	if runErr != nil {
		res.Err = fmt.Errorf("claude -p: %w", runErr)
		return res, res.Err
	}
	return res, nil
}

// childEnv builds the child environment. It mirrors the codex adapter: drop any
// inherited Go cache settings and point them inside the workspace, so the
// toolchain can write and the agent is never blocked by a read-only cache. Both
// paths are on the VCS scratch-exclusion list, so they cannot reach a commit.
func (r *Runner) childEnv(workDir string) []string {
	env := filterEnvKeys(os.Environ(), "GOCACHE", "GOPATH", "GH_TOKEN", "GITHUB_TOKEN")
	env = append(env, r.ExtraEnv...)
	if !hasEnvKey(r.ExtraEnv, "GOCACHE") {
		gc := filepath.Join(workDir, ".gocache")
		_ = os.MkdirAll(gc, 0o755)
		env = append(env, "GOCACHE="+gc)
	}
	if !hasEnvKey(r.ExtraEnv, "GOPATH") {
		gp := filepath.Join(workDir, ".gopath")
		_ = os.MkdirAll(gp, 0o755)
		env = append(env, "GOPATH="+gp)
	}
	return env
}

func filterEnvKeys(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		drop := false
		for _, k := range keys {
			if strings.HasPrefix(e, k+"=") {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, e)
		}
	}
	return out
}

func hasEnvKey(env []string, key string) bool {
	for _, e := range env {
		if strings.HasPrefix(e, key+"=") {
			return true
		}
	}
	return false
}

func tail(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
