package describe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validDiagram = "sequenceDiagram\n  A->>B: request\n  B-->>A: response"

func fullArtifact() Artifact {
	return Artifact{
		Walkthrough: "Adds a describer role.",
		Changes: []Change{
			{Cohort: "Roles", Summary: "New describer role and prompt."},
			{Cohort: "Loop", Summary: "Describe hook before commit."},
		},
		Diagrams: []string{validDiagram},
	}
}

func TestRenderFullArtifact(t *testing.T) {
	got := Render(fullArtifact())
	for _, want := range []string{
		BeginMarker, EndMarker,
		"## Walkthrough", "Adds a describer role.",
		"## Changes", "| Area | Summary |", "| Roles | New describer role and prompt. |",
		"## Sequence diagram", "```mermaid\n" + validDiagram + "\n```",
		"Generated description",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered body missing %q:\n%s", want, got)
		}
	}
}

// A diagram that fails validation is omitted; the rest of the body renders. This
// is the validate-or-omit guardrail: a bad diagram never breaks the PR body.
func TestRenderOmitsInvalidDiagrams(t *testing.T) {
	a := fullArtifact()
	a.Diagrams = []string{
		"flowchart TD\n  A --> B",       // not a sequenceDiagram
		"sequenceDiagram\n  ``` escape", // would escape its fence
		"",                              // empty
	}
	got := Render(a)
	if strings.Contains(got, "Sequence diagram") || strings.Contains(got, "mermaid") {
		t.Fatalf("invalid diagrams must be omitted:\n%s", got)
	}
	if !strings.Contains(got, "## Walkthrough") || !strings.Contains(got, "## Changes") {
		t.Fatalf("valid sections must survive an invalid diagram:\n%s", got)
	}
}

func TestRenderEmptyArtifactIsEmpty(t *testing.T) {
	if got := Render(Artifact{}); got != "" {
		t.Fatalf("empty artifact must render to \"\", got:\n%s", got)
	}
	// Rows with a missing half do not count as content.
	if got := Render(Artifact{Changes: []Change{{Cohort: "X"}}}); got != "" {
		t.Fatalf("half-empty change row must not render, got:\n%s", got)
	}
}

// Table cells must stay one line and must not break the table syntax.
func TestRenderFlattensTableCells(t *testing.T) {
	a := Artifact{Changes: []Change{{Cohort: "A|B", Summary: "line1\nline2"}}}
	got := Render(a)
	if !strings.Contains(got, `A\|B`) {
		t.Fatalf("pipe not escaped:\n%s", got)
	}
	if !strings.Contains(got, "line1 line2") {
		t.Fatalf("newline not flattened:\n%s", got)
	}
}

func TestRenderCapsDiagramCount(t *testing.T) {
	a := Artifact{Diagrams: []string{validDiagram, validDiagram, validDiagram, validDiagram}}
	got := Render(a)
	if n := strings.Count(got, "```mermaid"); n != maxDiagrams {
		t.Fatalf("diagram count = %d, want capped at %d", n, maxDiagrams)
	}
}

func TestLoadReadsArtifactFromScratchDir(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"walkthrough":"w","changes":[],"diagrams":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Load(work)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.Walkthrough != "w" {
		t.Fatalf("walkthrough = %q", a.Walkthrough)
	}
}

func TestLoadErrorsOnMissingOrMalformed(t *testing.T) {
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("missing artifact must error")
	}
	work := t.TempDir()
	path := filepath.Join(work, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(work); err == nil {
		t.Fatal("malformed artifact must error")
	}
}
