package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine/claude"
	"github.com/simplycubed/code/internal/engine/codex"
	forge2 "github.com/simplycubed/code/internal/forge"
	"github.com/simplycubed/code/internal/forge/dryrun"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
	vcsgit "github.com/simplycubed/code/internal/vcs/git"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/buildinfo"
)

func newTestFlagSet() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoDir := fs.String("repo-dir", ".", "")
	return fs, repoDir
}

// The documented usage is `run <owner/repo#N> [flags]`. Stdlib parsing stops at
// the first positional, which silently ignored trailing flags: --repo-dir stayed
// "." and the run failed later with a misleading config error.
func TestParseInterleavedHonorsFlagsAfterPositional(t *testing.T) {
	fs, repoDir := newTestFlagSet()
	rest, err := parseInterleaved(fs, []string{"owner/repo#1", "--repo-dir", "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *repoDir != "/x" {
		t.Fatalf("repo-dir = %q, want /x", *repoDir)
	}
	if len(rest) != 1 || rest[0] != "owner/repo#1" {
		t.Fatalf("positionals = %v, want [owner/repo#1]", rest)
	}
}

func TestParseInterleavedHonorsFlagsBeforePositional(t *testing.T) {
	fs, repoDir := newTestFlagSet()
	rest, err := parseInterleaved(fs, []string{"--repo-dir", "/x", "owner/repo#1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *repoDir != "/x" || len(rest) != 1 || rest[0] != "owner/repo#1" {
		t.Fatalf("repo-dir = %q positionals = %v", *repoDir, rest)
	}
}

func TestParseInterleavedRejectsUnknownFlags(t *testing.T) {
	fs, _ := newTestFlagSet()
	if _, err := parseInterleaved(fs, []string{"owner/repo#1", "--no-such-flag"}); err == nil {
		t.Fatal("expected an error for an unknown flag after the positional")
	}
}

func TestInitWritesStarterConfigAndLabels(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}

	configPath := filepath.Join(repoDir, ".github", "simplycubed.yml")
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(got)
	for _, want := range []string{"labelPrefix: sc", "gate:", "# Required. Fill this in with the real gate"} {
		if !strings.Contains(text, want) {
			t.Fatalf("starter config missing %q:\n%s", want, text)
		}
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	for _, want := range []string{
		"label list --limit 1000 --json name",
		"label create sc:go",
		"label create sc:queued",
		"label create sc:working",
		"label create sc:review",
		"label create sc:blocked",
		"label create sc:done",
	} {
		if !strings.Contains(string(logged), want) {
			t.Fatalf("gh log missing %q:\n%s", want, logged)
		}
	}

	output := out.String()
	for _, want := range []string{
		"wrote " + configPath,
		"created labels: sc:go, sc:queued, sc:working, sc:review, sc:blocked, sc:done",
		"install the simplycubed-code GitHub App with contents, issues, and pull requests permissions only",
		"disable the App webhook, set install visibility to Any account, and install it on the repo",
		"write the real gate in .github/simplycubed.yml",
		"verify that gate is green on your main branch",
		// Naming the section is the point: two of the four go under Variables
		// and two under Secrets, and a value filed on the wrong tab reads back
		// empty rather than erroring.
		"add two repository VARIABLES",
		"SIMPLYCUBED_GH_APP_CLIENT_ID       the App Client ID, the Iv23 string on the App settings page",
		"SIMPLYCUBED_AZURE_OPENAI_ENDPOINT  e.g. https://<resource>.openai.azure.com",
		"add two repository SECRETS",
		"SIMPLYCUBED_GH_APP_PRIVATE_KEY     the full PEM, including the BEGIN and END lines",
		"SIMPLYCUBED_AZURE_OPENAI_API_KEY   the Azure OpenAI key",
		"Variables and Secrets are different tabs",
		"merge the PR containing the config and workflow changes",
		"file an issue and apply the sc:go label",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestInitIsNoOpWhenConfigAndLabelsExist(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	configPath := filepath.Join(repoDir, ".github", "simplycubed.yml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	original := "gate: make check\n"
	if err := os.WriteFile(configPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[{"name":"sc:go"},{"name":"sc:queued"},{"name":"sc:working"},{"name":"sc:review"},{"name":"sc:blocked"},{"name":"sc:done"}]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}

	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(got) != original {
		t.Fatalf("config was overwritten:\n%s", got)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read gh log: %v", err)
	}
	if strings.Contains(string(logged), "label create") {
		t.Fatalf("existing labels should not be recreated:\n%s", logged)
	}

	output := out.String()
	if !strings.Contains(output, "left existing "+configPath+" unchanged") {
		t.Fatalf("expected existing-config message:\n%s", output)
	}
	if !strings.Contains(output, "labels already present: no changes") {
		t.Fatalf("expected no-op label message:\n%s", output)
	}
}

func TestInitWithWorkflowWritesPinnedCallerWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	buildVersionForTest(t, "0.1.0")

	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--workflow"}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}

	workflowPath := filepath.Join(repoDir, ".github", "workflows", "simplycubed.yml")
	got, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	workflow := string(got)
	if strings.Contains(workflow, callerWorkflowTagToken) {
		t.Fatalf("workflow still contains template token:\n%s", workflow)
	}
	for _, want := range []string{
		"uses: simplycubed/code/.github/workflows/simplycubed.yml@v0.1.0",
		"github-app-client-id: ${{ vars.SIMPLYCUBED_GH_APP_CLIENT_ID }}",
		"azure-openai-api-key: ${{ secrets.SIMPLYCUBED_AZURE_OPENAI_API_KEY }}",
		"github-app-private-key: ${{ secrets.SIMPLYCUBED_GH_APP_PRIVATE_KEY }}",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("workflow missing %q:\n%s", want, workflow)
		}
	}

	// The self-test is written alongside the caller, and the operator is told to
	// remove it: it is an install check, not part of normal operation.
	selftestPath := filepath.Join(repoDir, ".github", "workflows", "simplycubed-selftest.yml")
	if _, err := os.Stat(selftestPath); err != nil {
		t.Fatalf("self-test workflow not written: %v", err)
	}
	output := out.String()
	if !strings.Contains(output, "wrote "+workflowPath) {
		t.Fatalf("expected workflow write message:\n%s", output)
	}
	for _, want := range []string{"run the self-test once", "delete " + ".github/workflows/simplycubed-selftest.yml"} {
		if !strings.Contains(output, want) {
			t.Fatalf("next steps missing %q:\n%s", want, output)
		}
	}
}

func TestInitWithoutWorkflowDoesNotWriteCallerWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}

	workflowPath := filepath.Join(repoDir, ".github", "workflows", "simplycubed.yml")
	if _, err := os.Stat(workflowPath); !os.IsNotExist(err) {
		t.Fatalf("workflow file should not exist, stat err = %v", err)
	}
	if strings.Contains(out.String(), workflowPath) {
		t.Fatalf("output should not mention workflow path:\n%s", out.String())
	}
}

func TestInitWithWorkflowPreservesExistingWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	configPath := filepath.Join(repoDir, ".github", "simplycubed.yml")
	workflowPath := filepath.Join(repoDir, ".github", "workflows", "simplycubed.yml")
	if err := os.MkdirAll(filepath.Dir(workflowPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte("gate: make check\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalWorkflow := "name: keep-me\n"
	if err := os.WriteFile(workflowPath, []byte(originalWorkflow), 0o644); err != nil {
		t.Fatal(err)
	}

	stubDir := t.TempDir()
	logPath := filepath.Join(stubDir, "gh.log")
	stubPath := filepath.Join(stubDir, "gh")
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$GH_STUB_LOG"
if [ "$1 $2" = "label list" ]; then
  echo '[{"name":"sc:go"},{"name":"sc:queued"},{"name":"sc:working"},{"name":"sc:review"},{"name":"sc:blocked"},{"name":"sc:done"}]'
  exit 0
fi
exit 0
`
	if err := os.WriteFile(stubPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", logPath)
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var out bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--workflow"}, &out); err != nil {
		t.Fatalf("initCmd: %v", err)
	}

	got, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatalf("read workflow: %v", err)
	}
	if string(got) != originalWorkflow {
		t.Fatalf("workflow was overwritten:\n%s", got)
	}
	if !strings.Contains(out.String(), "left existing "+workflowPath+" unchanged") {
		t.Fatalf("expected existing-workflow message:\n%s", out.String())
	}
}

func TestWorkflowTemplateTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "release build pins exact tag", version: "0.2.3", want: "v0.2.3"},
		{name: "release build tolerates leading v", version: "v1.2.3", want: "v1.2.3"},
		{name: "dev build falls back to latest known tag", version: buildinfo.Version, want: latestKnownWorkflowTag},
		{name: "prerelease falls back to latest known tag", version: "0.2.3-rc1", want: latestKnownWorkflowTag},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := workflowTemplateTag(tt.version); got != tt.want {
				t.Fatalf("workflowTemplateTag(%q) = %q, want %q", tt.version, got, tt.want)
			}
		})
	}
}

func TestCallerWorkflowTemplateMatchesDocsTemplate(t *testing.T) {
	t.Parallel()

	want, err := os.ReadFile(filepath.Join("..", "..", "docs", "templates", "simplycubed-caller.yml"))
	if err != nil {
		t.Fatalf("read docs template: %v", err)
	}

	// Render at the latest known tag: the docs template pins the current
	// release, so a release bump that misses either copy fails here.
	got := []byte(renderCallerWorkflow(latestKnownWorkflowTag))
	if !bytes.Equal(got, want) {
		t.Fatalf("rendered embedded template does not match docs template\nrendered:\n%s\n\ndocs:\n%s", got, want)
	}
}

func buildVersionForTest(t *testing.T, version string) string {
	t.Helper()
	original := buildinfo.Version
	buildinfo.Version = version
	t.Cleanup(func() {
		buildinfo.Version = original
	})
	return original
}

func TestEngineEnvValidatesEndpointAndKey(t *testing.T) {
	set := func(endpoint, key string) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", endpoint)
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", key)
	}
	// A trailing slash is normalized away, because the engine appends a path.
	set("https://r.openai.azure.com/", "k")
	got, err := engineEnv(&config.Config{})
	if err != nil || got != "https://r.openai.azure.com" {
		t.Fatalf("engineEnv() = %q, %v", got, err)
	}
	// Each of these otherwise fails deep in the engine, where the message says
	// nothing about the cause.
	for name, c := range map[string][2]string{
		"missing endpoint": {"", "k"},
		"missing key":      {"https://r.openai.azure.com", ""},
		"not https":        {"http://r.openai.azure.com", "k"},
		"no scheme":        {"r.openai.azure.com", "k"},
		"no host":          {"https://", "k"},
	} {
		set(c[0], c[1])
		if _, err := engineEnv(&config.Config{}); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestEngineEnvSkipsAzureForClaude(t *testing.T) {
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "")
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
	got, err := engineEnv(&config.Config{Engine: "claude"})
	if err != nil || got != "" {
		t.Fatalf("engineEnv(claude) = %q, %v", got, err)
	}
}

func TestPreflightCmd(t *testing.T) {
	t.Run("reports ok when config and engine settings are present", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		var out bytes.Buffer
		if err := preflightCmd([]string{"--repo-dir", repoWithConfigBody(t, "gate: make check\n")}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "preflight ok") {
			t.Fatalf("output = %q", out.String())
		}
	})

	t.Run("allows claude with no Azure settings", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
		var out bytes.Buffer
		if err := preflightCmd([]string{"--repo-dir", repoWithConfigBody(t, "gate: make check\nengine: claude\n")}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "preflight ok") {
			t.Fatalf("output = %q", out.String())
		}
	})

	// The whole point of preflight is naming what is wrong, so each failure
	// asserts the message identifies the thing the operator has to fix.
	t.Run("names the missing config", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		err := preflightCmd([]string{"--repo-dir", t.TempDir()}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "load config") {
			t.Fatalf("err = %v, want a config error", err)
		}
	})

	t.Run("names the missing endpoint", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		err := preflightCmd([]string{"--repo-dir", repoWithConfigBody(t, "gate: make check\n")}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "SIMPLYCUBED_AZURE_OPENAI_ENDPOINT") {
			t.Fatalf("err = %v, want the endpoint named", err)
		}
	})

	t.Run("rejects an unknown flag", func(t *testing.T) {
		if err := preflightCmd([]string{"--nope"}, io.Discard); err == nil {
			t.Fatal("expected an error for an unknown flag")
		}
	})
}

func TestEngineEnvRejectsAnUnparseableEndpoint(t *testing.T) {
	// A control character makes url.Parse itself fail, which is a different
	// branch from the scheme and host checks.
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com/\x7f")
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
	if _, err := engineEnv(&config.Config{}); err == nil {
		t.Fatal("expected an error for an unparseable endpoint")
	}
}

func TestDispatchRoutesCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	// help is the one command that needs nothing else configured.
	if code := dispatch([]string{"command", "--body", "/simplycubed help"}, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "/simplycubed go") {
		t.Fatalf("stdout = %q, want the help text", out.String())
	}
}

func TestDispatch(t *testing.T) {
	// Every command's exit code is part of the contract: a workflow step
	// distinguishes "you asked for something that does not exist" (2) from
	// "the thing you asked for failed" (1).
	t.Run("version prints the version and succeeds", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := dispatch([]string{"version"}, &out, &errOut); code != 0 {
			t.Fatalf("exit = %d, want 0", code)
		}
		if !strings.Contains(out.String(), buildinfo.Version) {
			t.Fatalf("stdout = %q, want the version", out.String())
		}
	})

	t.Run("no arguments prints usage and exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := dispatch(nil, &out, &errOut); code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		if !strings.Contains(errOut.String(), "usage:") {
			t.Fatalf("stderr = %q, want usage", errOut.String())
		}
	})

	t.Run("an unknown command prints usage and exits 2", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := dispatch([]string{"nope"}, &out, &errOut); code != 2 {
			t.Fatalf("exit = %d, want 2", code)
		}
		if !strings.Contains(errOut.String(), "usage:") {
			t.Fatalf("stderr = %q, want usage", errOut.String())
		}
	})

	t.Run("init reports failure on stderr and exits 1", func(t *testing.T) {
		var out, errOut bytes.Buffer
		if code := dispatch([]string{"init", "--nope"}, &out, &errOut); code != 1 {
			t.Fatalf("exit = %d, want 1", code)
		}
		if !strings.Contains(errOut.String(), "error:") {
			t.Fatalf("stderr = %q, want an error", errOut.String())
		}
	})

	// A failing command exits 1 and says why on stderr, which is what an
	// operator reads out of a workflow log.
	for _, cmd := range []string{"preflight", "run", "address"} {
		t.Run(cmd+" reports failure on stderr and exits 1", func(t *testing.T) {
			t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "")
			t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
			var out, errOut bytes.Buffer
			// An empty directory has no config, so each command fails early.
			code := dispatch([]string{cmd, "--repo-dir", t.TempDir()}, &out, &errOut)
			if code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			if !strings.Contains(errOut.String(), "error:") {
				t.Fatalf("stderr = %q, want an error", errOut.String())
			}
		})
	}
}

func repoWithConfig(t *testing.T) string {
	t.Helper()
	return repoWithConfigBody(t, "gate: make check\nlabelPrefix: sc\n")
}

func repoWithConfigBody(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "simplycubed.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPrepare(t *testing.T) {
	t.Run("builds the dependency graph and returns the positional arguments", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		repo := repoWithConfig(t)
		c, rest, err := prepare("run", []string{"--repo-dir", repo, "--state-dir", t.TempDir(), "o/r#1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.repoDir != repo || c.cfg.Gate != "make check" {
			t.Fatalf("flags = %+v, cfg.Gate = %q", c, c.cfg.Gate)
		}
		if len(rest) != 1 || rest[0] != "o/r#1" {
			t.Fatalf("positionals = %v, want [o/r#1]", rest)
		}
		if c.deps.Runner == nil || c.deps.Forge == nil || c.deps.VCS == nil || c.deps.Worktrees == nil {
			t.Fatalf("dependency graph is incomplete: %+v", c.deps)
		}
	})

	// prepare is the common entry path, so each way the environment is wrong
	// has to stop here rather than surface later as an engine failure.
	t.Run("refuses a repo with no config", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		if _, _, err := prepare("run", []string{"--repo-dir", t.TempDir()}); err == nil {
			t.Fatal("expected an error for a repo with no config")
		}
	})

	t.Run("refuses a missing engine key", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
		_, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t)})
		if err == nil || !strings.Contains(err.Error(), "SIMPLYCUBED_AZURE_OPENAI_API_KEY") {
			t.Fatalf("err = %v, want the key named", err)
		}
	})

	t.Run("allows claude with no Azure settings and skips codex config", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
		stateDir := t.TempDir()
		c, _, err := prepare("run", []string{"--repo-dir", repoWithConfigBody(t, "gate: make check\nengine: claude\n"), "--state-dir", stateDir})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := c.deps.Runner.(*claude.Runner); !ok {
			t.Fatalf("expected the Claude runner, got %#v", c.deps.Runner)
		}
		if _, err := os.Stat(filepath.Join(stateDir, "codex-home", "config.toml")); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("claude runs must not render a codex config, stat err = %v", err)
		}
	})

	t.Run("rejects an unknown flag", func(t *testing.T) {
		if _, _, err := prepare("run", []string{"--nope"}); err == nil {
			t.Fatal("expected an error for an unknown flag")
		}
	})

	// --dry-run has to reach both the forge and the VCS, or a "dry" run would
	// still push.
	t.Run("dry-run wraps the forge and disables the push", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		c, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t), "--state-dir", t.TempDir(), "--dry-run"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.dry == nil {
			t.Fatal("a dry run must record the writes it skips")
		}
		v, ok := c.deps.VCS.(*vcsgit.Git)
		if !ok || !v.DryRun {
			t.Fatalf("a dry run must not push: %#v", c.deps.VCS)
		}
	})

	t.Run("a normal run does neither", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		c, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t), "--state-dir", t.TempDir()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if c.dry != nil {
			t.Fatal("a normal run must not be in dry-run mode")
		}
		if v, ok := c.deps.VCS.(*vcsgit.Git); !ok || v.DryRun {
			t.Fatal("a normal run must push")
		}
	})
}

func TestCommandCmdRoutesByCommentBody(t *testing.T) {
	t.Run("help replies without touching the repo", func(t *testing.T) {
		f := stubCommentForge(t, nil)
		var out bytes.Buffer
		defer func() {
			if len(f.Comments) != 1 || !strings.Contains(f.Comments[0], "/simplycubed go") {
				t.Fatalf("help must be answered on the thread, posted: %v", f.Comments)
			}
		}()
		if err := commandCmd([]string{"--body", "/simplycubed help", "o/r#1"}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "/simplycubed go") {
			t.Fatalf("help should list the commands, got: %q", out.String())
		}
	})

	// An unrecognised comment must do nothing at all, quietly. This is the case
	// that fires whenever anyone mentions the bot in passing.
	t.Run("an unrecognised comment does nothing", func(t *testing.T) {
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "thanks /simplycubed", "o/r#1"}, &out); err != nil {
			t.Fatalf("an unrecognised comment must not be an error: %v", err)
		}
		if !strings.Contains(out.String(), "nothing to do") {
			t.Fatalf("output = %q", out.String())
		}
	})
}

func TestReportDryRun(t *testing.T) {
	// A run that is not a dry run must say nothing at all about dry runs.
	t.Run("silent when not a dry run", func(t *testing.T) {
		var out bytes.Buffer
		reportDryRun(&commonFlags{}, &out)
		reportDryRun(nil, &out)
		if out.Len() != 0 {
			t.Fatalf("expected no output, got %q", out.String())
		}
	})

	// The report is the entire product of a dry run, and in Actions it has to
	// reach the run summary or nobody reads it.
	t.Run("prints the skipped writes and appends to the Actions summary", func(t *testing.T) {
		d := dryrun.New(&forgefake.Forge{})
		if _, err := d.OpenPR(context.Background(), "o/r", "sc/9", "Closes #9: t", "body"); err != nil {
			t.Fatal(err)
		}
		summary := filepath.Join(t.TempDir(), "summary.md")
		t.Setenv("GITHUB_STEP_SUMMARY", summary)

		var out bytes.Buffer
		reportDryRun(&commonFlags{dry: d}, &out)

		if !strings.Contains(out.String(), "open-pr") || !strings.Contains(out.String(), "Closes #9") {
			t.Fatalf("stdout missing the skipped write: %q", out.String())
		}
		b, err := os.ReadFile(summary)
		if err != nil {
			t.Fatalf("summary not written: %v", err)
		}
		if !strings.Contains(string(b), "SimplyCubed Code dry run") || !strings.Contains(string(b), "open-pr") {
			t.Fatalf("summary missing the report:\n%s", b)
		}
	})

	// A missing summary file must not break the run; it is an Actions nicety.
	t.Run("survives an unwritable summary path", func(t *testing.T) {
		d := dryrun.New(&forgefake.Forge{})
		t.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(t.TempDir(), "nope", "x.md"))
		var out bytes.Buffer
		reportDryRun(&commonFlags{dry: d}, &out)
		if out.Len() == 0 {
			t.Fatal("stdout should still get the report")
		}
	})
}

// Re-running init must never overwrite what an adopter has edited, including
// the self-test they may have deliberately deleted or changed.
func TestInitWithWorkflowIsIdempotent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	repoDir := t.TempDir()
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "gh")
	script := "#!/bin/sh\nif [ \"$1 $2\" = \"label list\" ]; then echo '[]'; fi\nexit 0\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GH_STUB_LOG", filepath.Join(stubDir, "gh.log"))
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var first bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--workflow"}, &first); err != nil {
		t.Fatalf("first init: %v", err)
	}
	selftest := filepath.Join(repoDir, ".github", "workflows", "simplycubed-selftest.yml")
	if err := os.WriteFile(selftest, []byte("# edited by the adopter\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var second bytes.Buffer
	if err := initCmd([]string{"--repo-dir", repoDir, "--workflow"}, &second); err != nil {
		t.Fatalf("second init: %v", err)
	}
	if !strings.Contains(second.String(), "left existing "+selftest+" unchanged") {
		t.Fatalf("second run should report the self-test unchanged:\n%s", second.String())
	}
	got, err := os.ReadFile(selftest)
	if err != nil || !strings.Contains(string(got), "edited by the adopter") {
		t.Fatalf("the adopter's edit must survive: %q %v", got, err)
	}
}

func TestTakeBody(t *testing.T) {
	// The other flags belong to run and address and must survive untouched,
	// or routing a comment would drop --actor and skip authorization.
	for name, tc := range map[string]struct {
		argv     []string
		wantBody string
		wantRest []string
	}{
		"separate value": {
			[]string{"--body", "/simplycubed go", "--actor", "me", "o/r#1"},
			"/simplycubed go", []string{"--actor", "me", "o/r#1"},
		},
		"equals form": {
			[]string{"--body=/simplycubed address", "--dry-run", "o/r#2"},
			"/simplycubed address", []string{"--dry-run", "o/r#2"},
		},
		"absent": {
			[]string{"--actor", "me", "o/r#1"}, "", []string{"--actor", "me", "o/r#1"},
		},
	} {
		body, rest, err := takeBody(tc.argv)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if body != tc.wantBody {
			t.Fatalf("%s: body = %q, want %q", name, body, tc.wantBody)
		}
		if strings.Join(rest, " ") != strings.Join(tc.wantRest, " ") {
			t.Fatalf("%s: rest = %v, want %v", name, rest, tc.wantRest)
		}
	}
	if _, _, err := takeBody([]string{"--body"}); err == nil {
		t.Fatal("a --body with no value must error rather than silently parse as empty")
	}
}

// The parser exists so that a comment the agent does not understand does
// nothing. Before this was wired, any comment beginning with the mention
// started a full run, including one asking it not to.
// stubCommentForge makes the reply path answer a fake instead of GitHub, and
// returns it so a test can read what was posted.
func stubCommentForge(t *testing.T, prs map[int]bool) *forgefake.Forge {
	t.Helper()
	f := &forgefake.Forge{PullRequests: prs}
	prev := newCommentForge
	newCommentForge = func() forge2.Forge { return f }
	t.Cleanup(func() { newCommentForge = prev })
	return f
}

func TestCommandCmdDoesNotRunOnUnrecognisedComments(t *testing.T) {
	// Addressed to the agent, but carrying no verb it knows. It must not run,
	// and it must not leave the person guessing either: the reply names the
	// vocabulary. "please do not do this one" is the case that matters most,
	// because it once started the work it was asking not to do.
	t.Run("addressed but unrecognised gets an answer", func(t *testing.T) {
		for _, body := range []string{
			"/simplycubed please do not do this one",
			"/simplycubed",
		} {
			f := stubCommentForge(t, nil)
			var out bytes.Buffer
			err := commandCmd([]string{"--body", body, "--repo-dir", t.TempDir(), "o/r#1"}, &out)
			if err != nil {
				t.Fatalf("%q should not be an error, got: %v", body, err)
			}
			if len(f.Comments) != 1 {
				t.Fatalf("%q should be answered on the thread, posted: %v", body, f.Comments)
			}
			if !strings.Contains(f.Comments[0], "/simplycubed go") {
				t.Fatalf("%q: the answer should name the vocabulary, got: %q", body, f.Comments[0])
			}
		}
	})

	// Not addressed to the agent at all. Answering this would make it the
	// noisiest participant in every thread it is ever mentioned in.
	t.Run("a passing mention stays silent", func(t *testing.T) {
		f := stubCommentForge(t, nil)
		var out bytes.Buffer
		err := commandCmd([]string{"--body", "thanks /simplycubed go", "--repo-dir", t.TempDir(), "o/r#1"}, &out)
		if err != nil {
			t.Fatalf("should be a quiet no-op, got: %v", err)
		}
		if len(f.Comments) != 0 || len(f.PRComments) != 0 {
			t.Fatalf("a passing mention must post nothing, posted: %v %v", f.Comments, f.PRComments)
		}
		if !strings.Contains(out.String(), "nothing to do") {
			t.Fatalf("output = %q", out.String())
		}
	})
}

// The verb has to match the surface it was aimed at. GitHub numbers issues and
// pull requests from one sequence, so nothing in the ref says which it is, and
// before this check `address` on an issue failed with a raw GraphQL error that
// nobody saw.
func TestCommandCmdAnswersAVerbAimedAtTheWrongSurface(t *testing.T) {
	t.Run("address on an issue names go", func(t *testing.T) {
		f := stubCommentForge(t, nil) // #1 is an issue
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "/simplycubed address this issue.", "--repo-dir", t.TempDir(), "o/r#1"}, &out); err != nil {
			t.Fatalf("a wrong verb must not fail the run: %v", err)
		}
		if len(f.Comments) != 1 {
			t.Fatalf("should have answered on the issue, posted: %v", f.Comments)
		}
		if !strings.Contains(f.Comments[0], "/simplycubed go") {
			t.Fatalf("the answer must name the verb that applies, got: %q", f.Comments[0])
		}
	})

	t.Run("go on a pull request names address", func(t *testing.T) {
		f := stubCommentForge(t, map[int]bool{7: true})
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "/simplycubed go", "--repo-dir", t.TempDir(), "o/r#7"}, &out); err != nil {
			t.Fatalf("a wrong verb must not fail the run: %v", err)
		}
		if len(f.PRComments) != 1 {
			t.Fatalf("should have answered on the pull request, posted: %v", f.PRComments)
		}
		if !strings.Contains(f.PRComments[0], "/simplycubed address") {
			t.Fatalf("the answer must name the verb that applies, got: %q", f.PRComments[0])
		}
	})

	// A dry run exercises the path and posts nothing, like every other write.
	t.Run("a dry run records the answer instead of posting it", func(t *testing.T) {
		f := stubCommentForge(t, nil)
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "/simplycubed address", "--dry-run", "--repo-dir", t.TempDir(), "o/r#1"}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.Comments) != 0 {
			t.Fatalf("a dry run must post nothing, posted: %v", f.Comments)
		}
		if !strings.Contains(out.String(), "comment") {
			t.Fatalf("the dry-run report should name the skipped comment, got: %q", out.String())
		}
	})
}

// The routing branches are the point of the subcommand: a recognised verb has
// to reach the matching loop, carrying the flags with it. Each case uses a
// directory with no config, so reaching the loop fails at config load - which
// is how we know it was reached at all.
func TestCommandCmdRoutesRecognisedVerbsToTheirLoops(t *testing.T) {
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")

	for name, body := range map[string]string{
		"go routes to run":          "/simplycubed go",
		"address routes to address": "/simplycubed address",
	} {
		// #1 is an issue and #7 a pull request, so each verb is aimed at the
		// surface it applies to and reaches its loop rather than being answered.
		ref := "o/r#1"
		if strings.Contains(body, "address") {
			ref = "o/r#7"
		}
		stubCommentForge(t, map[int]bool{7: true})
		var out bytes.Buffer
		err := commandCmd([]string{"--body", body, "--repo-dir", t.TempDir(), ref}, &out)
		if err == nil {
			t.Fatalf("%s: expected the loop to be reached and fail on the missing config", name)
		}
		if !strings.Contains(err.Error(), "load config") {
			t.Fatalf("%s: err = %v, want a config error proving the loop was reached", name, err)
		}
		if strings.Contains(out.String(), "nothing to do") {
			t.Fatalf("%s: a recognised verb must not be treated as a no-op", name)
		}
	}
}

func TestCommandCmdRejectsABodyFlagWithNoValue(t *testing.T) {
	if err := commandCmd([]string{"--body"}, io.Discard); err == nil {
		t.Fatal("expected an error for --body with no value")
	}
}

// Engine selection is a documented config key and had no test. The default
// matters most: an existing repository must keep the engine it already had.
func TestNewRunnerSelectsTheEngine(t *testing.T) {
	if _, ok := newRunner(&config.Config{}, t.TempDir()).(*codex.Runner); !ok {
		t.Fatal("the default engine must be codex")
	}
	if _, ok := newRunner(&config.Config{Engine: "claude"}, t.TempDir()).(*claude.Runner); !ok {
		t.Fatal("engine: claude must select the Claude adapter")
	}
	// An unrecognised engine falls back rather than failing later with an
	// error that says nothing about the cause.
	if _, ok := newRunner(&config.Config{Engine: "nope"}, t.TempDir()).(*codex.Runner); !ok {
		t.Fatal("an unknown engine must fall back to the default")
	}
}

// The sandbox knob exists so an adopter with externally sandboxed runners can
// widen it themselves. Nothing in this repository sets it, so the test asserts
// both that the default is untouched and that an explicit value is honoured.
func TestNewRunnerHonoursTheSandboxOverride(t *testing.T) {
	r, ok := newRunner(&config.Config{}, t.TempDir()).(*codex.Runner)
	if !ok {
		t.Fatal("expected the codex runner")
	}
	if r.Sandbox != "workspace-write" {
		t.Fatalf("the sandbox must stay on by default, got %q", r.Sandbox)
	}
	t.Setenv("SIMPLYCUBED_SANDBOX", "read-only")
	r2, _ := newRunner(&config.Config{}, t.TempDir()).(*codex.Runner)
	if r2.Sandbox != "read-only" {
		t.Fatalf("an explicit sandbox mode must be honoured, got %q", r2.Sandbox)
	}
}

// A local run has no bot identity to attribute commits to, and must not invent
// one; an Actions run does, and a runner has no git identity without it.
func TestNewVCSAttributesCommitsToTheCredential(t *testing.T) {
	if v := newVCS(""); v.AuthorName != "" || v.AuthorEmail != "" {
		t.Fatalf("a run with no known identity must not invent one: %+v", v)
	}
	v := newVCS("simplycubed-code[bot]")
	if v.AuthorName != "simplycubed-code[bot]" {
		t.Fatalf("AuthorName = %q", v.AuthorName)
	}
	if !strings.HasSuffix(v.AuthorEmail, "@users.noreply.github.com") || strings.Contains(v.AuthorEmail, "[bot]") {
		t.Fatalf("AuthorEmail = %q, want a noreply address with the bot suffix stripped", v.AuthorEmail)
	}
}

func TestIsReleaseVersion(t *testing.T) {
	for in, want := range map[string]bool{
		"0.1.6": true, "10.20.30": true,
		"0.1": false, "0.1.6-rc1": false, "": false, "a.b.c": false, "0..6": false,
	} {
		if got := isReleaseVersion(in); got != want {
			t.Fatalf("isReleaseVersion(%q) = %v, want %v", in, got, want)
		}
	}
}

// The issue title and body become the prompt, so fetching them wrong is not a
// cosmetic failure: the agent would work from the wrong instructions.
func TestFetchIssue(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("gh stub is a POSIX shell script")
	}
	stubDir := t.TempDir()
	stub := filepath.Join(stubDir, "gh")
	body := `{"title":"Fix the parser","body":"It drops the last line."}`
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat <<'J'\n"+body+"\nJ\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	iss := domain.Issue{Repo: "o/r", Number: 12}
	if err := fetchIssue(&iss); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if iss.Title != "Fix the parser" || iss.Body != "It drops the last line." {
		t.Fatalf("issue = %+v", iss)
	}

	// Malformed output must be an error, not a silently empty prompt.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\necho 'not json'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fetchIssue(&domain.Issue{Repo: "o/r", Number: 1}); err == nil {
		t.Fatal("malformed gh output must error")
	}

	// A gh failure must surface rather than leaving the issue blank.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fetchIssue(&domain.Issue{Repo: "o/r", Number: 1}); err == nil {
		t.Fatal("a gh failure must error")
	}
}

func TestRunAndAddressRequireARef(t *testing.T) {
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
	t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
	repo := repoWithConfig(t)
	for name, fn := range map[string]func([]string) error{"run": runCmd, "address": addressCmd} {
		err := fn([]string{"--repo-dir", repo, "--state-dir", t.TempDir()})
		if err == nil || !strings.Contains(err.Error(), "missing") {
			t.Fatalf("%s with no ref: err = %v, want a missing-ref error", name, err)
		}
	}
}

func TestAnswerPathEdges(t *testing.T) {
	// Driven by hand with no ref, there is nothing to answer on, so the local
	// echo is the whole answer and it must not be an error.
	t.Run("no ref means echo only", func(t *testing.T) {
		f := stubCommentForge(t, nil)
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "/simplycubed help"}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.Comments) != 0 {
			t.Fatalf("nothing to post to, posted: %v", f.Comments)
		}
		if !strings.Contains(out.String(), "/simplycubed go") {
			t.Fatalf("the answer should still reach stdout, got %q", out.String())
		}
	})

	// A lookup failure must not be silently treated as "this is an issue",
	// which would send the fixer at the wrong surface anyway.
	t.Run("a failed surface lookup is an error", func(t *testing.T) {
		prev := newCommentForge
		newCommentForge = func() forge2.Forge {
			return &forgefake.Forge{IsPRErr: errors.New("github said no")}
		}
		t.Cleanup(func() { newCommentForge = prev })
		var out bytes.Buffer
		err := commandCmd([]string{"--body", "/simplycubed address", "o/r#1"}, &out)
		if err == nil {
			t.Fatal("want an error when the surface cannot be resolved")
		}
		if !strings.Contains(err.Error(), "resolve o/r#1") {
			t.Fatalf("the error should name what it could not resolve, got: %v", err)
		}
		// The same must hold on the reply path, which help and an unrecognised
		// comment both take.
		if err := commandCmd([]string{"--body", "/simplycubed help", "o/r#1"}, &out); err == nil {
			t.Fatal("want an error on the reply path too")
		}
	})

	t.Run("the dry-run environment variable is honoured", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_DRY_RUN", "1")
		f := stubCommentForge(t, nil)
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "/simplycubed address", "o/r#1"}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.Comments) != 0 {
			t.Fatalf("a dry run must post nothing, posted: %v", f.Comments)
		}
	})
}

// The workflow-file preflight used to compare the authenticated login against a
// hardcoded "simplycubed-code[bot]". Every adopter's App carries a different
// name, because GitHub App names are globally unique, so the comparison was
// false for everyone but us and the preflight never ran where it was needed.
func TestWorkflowRestrictedPushCoversAnyBotIdentity(t *testing.T) {
	t.Run("an adopter's own bot is restricted", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		t.Setenv("SIMPLYCUBED_GH_APP_LOGIN", "acme-code[bot]")
		c, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t), "--state-dir", t.TempDir(), "o/r#1"})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if !c.deps.WorkflowRestrictedPush {
			t.Fatal("a bot identity that is not ours must still be workflow-restricted")
		}
		if c.deps.SelfLogin != "acme-code[bot]" {
			t.Fatalf("SelfLogin = %q, want the authenticated login so the escalation can name it", c.deps.SelfLogin)
		}
	})

	t.Run("a human running locally is not restricted", func(t *testing.T) {
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		t.Setenv("SIMPLYCUBED_GH_APP_LOGIN", "")
		c, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t), "--state-dir", t.TempDir(), "o/r#1"})
		if err != nil {
			t.Fatalf("prepare: %v", err)
		}
		if c.deps.WorkflowRestrictedPush {
			t.Fatal("an empty login is a human under their own credential, who can push workflow files")
		}
	})
}

// Two of the four adopter-set values live under Variables and two under
// Secrets. A value filed on the wrong tab reads back empty rather than
// erroring, so preflight has to catch it and say which tab it belongs on.
func TestPreflightChecksTheAdopterSetValues(t *testing.T) {
	setAll := func(t *testing.T) {
		t.Helper()
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "k")
		t.Setenv("SIMPLYCUBED_GH_APP_CLIENT_ID", "Iv23example")
		t.Setenv("SIMPLYCUBED_GH_APP_PRIVATE_KEY", "-----BEGIN...")
	}

	for _, tc := range []struct {
		name, unset, wantSection string
	}{
		{"endpoint", "SIMPLYCUBED_AZURE_OPENAI_ENDPOINT", "repository variable"},
		{"api key", "SIMPLYCUBED_AZURE_OPENAI_API_KEY", "repository secret"},
		{"client id", "SIMPLYCUBED_GH_APP_CLIENT_ID", "repository variable"},
		{"private key", "SIMPLYCUBED_GH_APP_PRIVATE_KEY", "repository secret"},
	} {
		t.Run(tc.name+" missing names its section", func(t *testing.T) {
			setAll(t)
			t.Setenv(tc.unset, "")
			err := preflightCmd([]string{"--repo-dir", repoWithConfig(t), "--actions"}, io.Discard)
			if err == nil {
				t.Fatalf("%s unset must fail preflight", tc.unset)
			}
			if !errors.Is(err, ErrConfigMissing) {
				t.Fatalf("err = %v, want it to classify as a configuration miss", err)
			}
			for _, want := range []string{tc.unset, tc.wantSection} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("err = %q, expected it to mention %q", err, want)
				}
			}
		})
	}

	t.Run("a value filed on the wrong tab looks empty and fails the same way", func(t *testing.T) {
		setAll(t)
		t.Setenv("SIMPLYCUBED_GH_APP_CLIENT_ID", "   ")
		err := preflightCmd([]string{"--repo-dir", repoWithConfig(t), "--actions"}, io.Discard)
		if !errors.Is(err, ErrConfigMissing) {
			t.Fatalf("err = %v, want a whitespace-only value treated as unset", err)
		}
	})

	t.Run("a local run does not require the App pair", func(t *testing.T) {
		setAll(t)
		t.Setenv("SIMPLYCUBED_GH_APP_CLIENT_ID", "")
		t.Setenv("SIMPLYCUBED_GH_APP_PRIVATE_KEY", "")
		if err := preflightCmd([]string{"--repo-dir", repoWithConfig(t)}, io.Discard); err != nil {
			t.Fatalf("a local run authenticates as the operator and never uses the App: %v", err)
		}
	})

	t.Run("a configuration miss exits 3, not 1", func(t *testing.T) {
		setAll(t)
		t.Setenv("SIMPLYCUBED_AZURE_OPENAI_API_KEY", "")
		var out, errOut bytes.Buffer
		if code := dispatch([]string{"preflight", "--repo-dir", repoWithConfig(t)}, &out, &errOut); code != 3 {
			t.Fatalf("exit = %d, want 3 so a caller can tell a missing value from a bug", code)
		}
	})
}
