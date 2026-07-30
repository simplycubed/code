package main

import (
	"bytes"
	"flag"
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
		"write the real gate in .github/simplycubed.yml",
		"verify that gate is green on your main branch",
		"set the AZURE_OPENAI_ENDPOINT repo variable",
		"add the AZURE_OPENAI_API_KEY repo secret",
		"optionally add the SIMPLYCUBED_GH_TOKEN repo secret",
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
		"azure-openai-api-key: ${{ secrets.AZURE_OPENAI_API_KEY }}",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("workflow missing %q:\n%s", want, workflow)
		}
	}

	output := out.String()
	if !strings.Contains(output, "wrote "+workflowPath) {
		t.Fatalf("expected workflow write message:\n%s", output)
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

func buildVersionForTest(t *testing.T, version string) string {
	t.Helper()
	original := buildinfo.Version
	buildinfo.Version = version
	t.Cleanup(func() {
		buildinfo.Version = original
	})
	return original
}
