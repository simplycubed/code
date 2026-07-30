// Package codex implements engine.Runner by shelling out to the OpenAI Codex CLI
// (`codex exec`). It is the first real engine adapter; it targets Azure OpenAI
// through a codex config (a CODEX_HOME the caller supplies or generates), and the
// loop chooses the model per request.
//
// Two lessons from the Phase 0 spikes are encoded here (see docs/decisions/0003):
//
//   - The sandbox must be pre-configured so the agent fixes the bug, not the
//     environment. The caller passes ExtraEnv (for example a workspace-local
//     GOCACHE and signing disabled) so the toolchain can run without the agent
//     being tempted to edit the gate to get past a sandbox limit.
//   - This adapter only runs the turn. It does NOT police what the agent changed.
//     Guarding the gate and test files against agent edits is a separate, path-
//     based control (the PR guard), because an agent told not to touch one thing
//     will change another.
package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/simplycubed/code/internal/domain"
)

// Runner runs one role turn via `codex exec`.
type Runner struct {
	// Bin is the codex binary. Defaults to "codex" (resolved on PATH).
	Bin string
	// CodexHome is the CODEX_HOME directory holding the provider config. Required.
	CodexHome string
	// Sandbox is the codex sandbox mode. Defaults to "workspace-write".
	Sandbox string
	// Profile, if set, is passed as --profile.
	Profile string
	// ExtraEnv is appended to the child process environment, for pre-configuring
	// the sandbox (for example "GOCACHE=/path/inside/workspace"). See the package
	// doc for why this matters.
	ExtraEnv []string
	// DisableMCP passes an empty mcp_servers override, so a developer's desktop
	// Codex MCP servers do not start during a headless run. Default true.
	DisableMCP bool
}

// New returns a Runner with the defaults applied.
func New(codexHome string) *Runner {
	return &Runner{CodexHome: codexHome, Sandbox: "workspace-write", DisableMCP: true}
}

func (r *Runner) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	return "codex"
}

func (r *Runner) sandbox() string {
	if r.Sandbox != "" {
		return r.Sandbox
	}
	return "workspace-write"
}

// Run executes one turn. It captures the agent's final message via
// --output-last-message and returns it as the RunResult summary. A non-nil error
// (and RunResult.Err) means codex itself failed; it does not mean a gate failed.
func (r *Runner) Run(ctx context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if r.CodexHome == "" {
		err := errors.New("codex: CodexHome is required")
		return domain.RunResult{Role: req.Role, Err: err}, err
	}

	lastMsg, err := os.CreateTemp("", "sc-lastmsg-*.txt")
	if err != nil {
		return domain.RunResult{Role: req.Role, Err: err}, err
	}
	lastPath := lastMsg.Name()
	_ = lastMsg.Close()
	defer os.Remove(lastPath)

	args := []string{
		"exec",
		"--skip-git-repo-check",
		"--color", "never",
		"-s", r.sandbox(),
		"-C", req.WorkDir,
		"-o", lastPath,
	}
	if r.DisableMCP {
		args = append(args, "-c", "mcp_servers={}")
	}
	if r.Profile != "" {
		args = append(args, "--profile", r.Profile)
	}
	if req.Model != "" {
		args = append(args, "-m", req.Model)
	}
	args = append(args, req.Prompt)

	cmd := exec.CommandContext(ctx, r.bin(), args...)
	cmd.Dir = req.WorkDir
	cmd.Env = append(os.Environ(), r.ExtraEnv...)
	cmd.Env = append(cmd.Env, "CODEX_HOME="+r.CodexHome)
	out, runErr := cmd.CombinedOutput()

	summary := ""
	if b, e := os.ReadFile(lastPath); e == nil {
		summary = strings.TrimSpace(string(b))
	}
	if summary == "" {
		summary = tail(string(out), 20)
	}

	res := domain.RunResult{Role: req.Role, Summary: summary}
	if runErr != nil {
		res.Err = fmt.Errorf("codex exec: %w", runErr)
		return res, res.Err
	}
	return res, nil
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
