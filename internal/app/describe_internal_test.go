package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
	enginefake "github.com/simplycubed/code/internal/engine/fake"
)

func writeArtifact(json string) func(string) error {
	return func(dir string) error {
		path := filepath.Join(dir, ".simplycubed", "describe.json")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(json), 0o644)
	}
}

func TestDescribeHookRendersArtifact(t *testing.T) {
	work := t.TempDir()
	r := enginefake.New(enginefake.Step{
		Summary: "described",
		Apply:   writeArtifact(`{"walkthrough":"What changed and why.","changes":[{"cohort":"Docs","summary":"Updated."}]}`),
	})
	md := describeHook(r, work)(context.Background(), domain.Issue{Repo: "o/r", Number: 5, Title: "t"})
	for _, want := range []string{"## Walkthrough", "What changed and why.", "| Docs | Updated. |"} {
		if !strings.Contains(md, want) {
			t.Fatalf("hook output missing %q:\n%s", want, md)
		}
	}
}

// Best-effort contract: an engine failure, a missing artifact, or a malformed
// artifact all mean "no description", never an error that blocks the PR.
func TestDescribeHookFailuresRenderEmpty(t *testing.T) {
	cases := map[string]*enginefake.Runner{
		"engine error":   enginefake.New(enginefake.Step{Err: errors.New("boom")}),
		"no artifact":    enginefake.New(enginefake.Step{Summary: "wrote nothing"}),
		"malformed":      enginefake.New(enginefake.Step{Summary: "bad", Apply: writeArtifact("not json")}),
		"empty artifact": enginefake.New(enginefake.Step{Summary: "empty", Apply: writeArtifact("{}")}),
	}
	for name, r := range cases {
		if md := describeHook(r, t.TempDir())(context.Background(), domain.Issue{}); md != "" {
			t.Fatalf("%s: want empty output, got:\n%s", name, md)
		}
	}
}
