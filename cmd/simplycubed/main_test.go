package main

import (
	"bytes"
	"context"
	"flag"
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
		"set the SIMPLYCUBED_GH_APP_ID repo variable",
		"add the SIMPLYCUBED_GH_APP_PRIVATE_KEY repo secret with the full PEM, including BEGIN/END lines",
		"set the AZURE_OPENAI_ENDPOINT repo variable",
		"add the AZURE_OPENAI_API_KEY repo secret",
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
		"github-app-id: ${{ vars.SIMPLYCUBED_GH_APP_ID }}",
		"azure-openai-api-key: ${{ secrets.AZURE_OPENAI_API_KEY }}",
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
		t.Setenv("AZURE_OPENAI_ENDPOINT", endpoint)
		t.Setenv("AZURE_OPENAI_API_KEY", key)
	}
	// A trailing slash is normalized away, because the engine appends a path.
	set("https://r.openai.azure.com/", "k")
	got, err := engineEnv()
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
		if _, err := engineEnv(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestPreflightCmd(t *testing.T) {
	writeConfig := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		path := filepath.Join(dir, ".github", "simplycubed.yml")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("gate: make check\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("reports ok when config and engine settings are present", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
		var out bytes.Buffer
		if err := preflightCmd([]string{"--repo-dir", writeConfig(t)}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "preflight ok") {
			t.Fatalf("output = %q", out.String())
		}
	})

	// The whole point of preflight is naming what is wrong, so each failure
	// asserts the message identifies the thing the operator has to fix.
	t.Run("names the missing config", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
		err := preflightCmd([]string{"--repo-dir", t.TempDir()}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "load config") {
			t.Fatalf("err = %v, want a config error", err)
		}
	})

	t.Run("names the missing endpoint", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_ENDPOINT", "")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
		err := preflightCmd([]string{"--repo-dir", writeConfig(t)}, io.Discard)
		if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_ENDPOINT") {
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
	t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com/\x7f")
	t.Setenv("AZURE_OPENAI_API_KEY", "k")
	if _, err := engineEnv(); err == nil {
		t.Fatal("expected an error for an unparseable endpoint")
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
			t.Setenv("AZURE_OPENAI_ENDPOINT", "")
			t.Setenv("AZURE_OPENAI_API_KEY", "")
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
	dir := t.TempDir()
	path := filepath.Join(dir, ".github", "simplycubed.yml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("gate: make check\nlabelPrefix: sc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPrepare(t *testing.T) {
	t.Run("builds the dependency graph and returns the positional arguments", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
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
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
		if _, _, err := prepare("run", []string{"--repo-dir", t.TempDir()}); err == nil {
			t.Fatal("expected an error for a repo with no config")
		}
	})

	t.Run("refuses a missing engine key", func(t *testing.T) {
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "")
		_, _, err := prepare("run", []string{"--repo-dir", repoWithConfig(t)})
		if err == nil || !strings.Contains(err.Error(), "AZURE_OPENAI_API_KEY") {
			t.Fatalf("err = %v, want the key named", err)
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
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
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
		t.Setenv("AZURE_OPENAI_ENDPOINT", "https://r.openai.azure.com")
		t.Setenv("AZURE_OPENAI_API_KEY", "k")
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
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "@simplycubed-code help", "o/r#1"}, &out); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(out.String(), "@simplycubed-code go") {
			t.Fatalf("help should list the commands, got: %q", out.String())
		}
	})

	// An unrecognised comment must do nothing at all, quietly. This is the case
	// that fires whenever anyone mentions the bot in passing.
	t.Run("an unrecognised comment does nothing", func(t *testing.T) {
		var out bytes.Buffer
		if err := commandCmd([]string{"--body", "thanks @simplycubed-code", "o/r#1"}, &out); err != nil {
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
			[]string{"--body", "@simplycubed-code go", "--actor", "me", "o/r#1"},
			"@simplycubed-code go", []string{"--actor", "me", "o/r#1"},
		},
		"equals form": {
			[]string{"--body=@simplycubed-code address", "--dry-run", "o/r#2"},
			"@simplycubed-code address", []string{"--dry-run", "o/r#2"},
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
func TestCommandCmdDoesNotRunOnUnrecognisedComments(t *testing.T) {
	for _, body := range []string{
		"@simplycubed-code please do not do this one",
		"thanks @simplycubed-code go",
		"@simplycubed-code",
	} {
		var out bytes.Buffer
		// A repo-dir that has no config would make a real run fail loudly, so
		// reaching one is detectable.
		err := commandCmd([]string{"--body", body, "--repo-dir", t.TempDir(), "o/r#1"}, &out)
		if err != nil {
			t.Fatalf("%q should be a quiet no-op, got: %v", body, err)
		}
		if !strings.Contains(out.String(), "nothing to do") {
			t.Fatalf("%q should report nothing to do, got: %q", body, out.String())
		}
	}
}
