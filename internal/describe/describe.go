// Package describe turns an engine-produced artifact into the rich pull-request
// description of issue #16: a walkthrough, a changes table, and mermaid sequence
// diagrams. The engine writes the artifact as JSON to a file inside the
// .simplycubed scratch directory; the loop reads it back before the commit step,
// which then deletes the scratch directory, so the artifact is structurally
// incapable of leaking into the pull request.
//
// Everything here is best-effort by contract: a missing, malformed, or partly
// invalid artifact renders to less output, never to an error the loop must
// handle. The pull request always opens; the description is additive.
package describe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RelPath is the artifact path relative to the working tree, as given to the
// engine in the describer prompt. It lives under the .simplycubed scratch
// directory, which the VCS deletes before staging, so it can never be committed.
const RelPath = ".simplycubed/describe.json"

// Change is one row of the changes table: a semantic area and what changed in it.
type Change struct {
	Cohort  string `json:"cohort"`
	Summary string `json:"summary"`
}

// Artifact is the structured description the describer role produces.
type Artifact struct {
	Walkthrough string   `json:"walkthrough"`
	Changes     []Change `json:"changes"`
	Diagrams    []string `json:"diagrams"`
}

// Load reads and parses the artifact from the working tree. It returns an error
// for a missing or malformed file; callers treat any error as "no description".
func Load(workDir string) (Artifact, error) {
	b, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(RelPath)))
	if err != nil {
		return Artifact{}, err
	}
	return Parse(b)
}

// Parse parses artifact JSON.
func Parse(b []byte) (Artifact, error) {
	var a Artifact
	if err := json.Unmarshal(b, &a); err != nil {
		return Artifact{}, fmt.Errorf("describe: parse artifact: %w", err)
	}
	return a, nil
}

// Bounds on generated content. Models sometimes emit runaway output; these caps
// keep a bad artifact from bloating a pull-request body.
const (
	maxDiagrams     = 3
	maxDiagramLines = 120
	maxDiagramBytes = 8 << 10
)

// validMermaid is the validate-or-omit gate for a generated diagram. It is a
// structural check, not a mermaid parser: the diagram must declare itself a
// sequenceDiagram, must not escape its fence, and must be bounded in size. A
// diagram that fails is omitted; it never breaks the body.
func validMermaid(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxDiagramBytes {
		return false
	}
	if strings.Contains(s, "```") {
		return false
	}
	lines := strings.Split(s, "\n")
	if len(lines) > maxDiagramLines {
		return false
	}
	return strings.HasPrefix(lines[0], "sequenceDiagram")
}

// Markers delimit the generated sections in the pull-request body, so a later
// regeneration (for example after a fixer push) can replace exactly its own
// output without touching human edits around it.
const (
	BeginMarker = "<!-- simplycubed:describe -->"
	EndMarker   = "<!-- /simplycubed:describe -->"
)

// tableCell flattens a value for a one-line markdown table cell.
func tableCell(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.TrimSpace(s)
}

// Render assembles the markdown for the pull-request body. Sections with no
// valid content are omitted; if nothing survives, it returns "" and the caller
// uses the plain body.
func Render(a Artifact) string {
	var sections []string

	if w := strings.TrimSpace(a.Walkthrough); w != "" {
		sections = append(sections, "## Walkthrough\n\n"+w)
	}

	var rows []string
	for _, c := range a.Changes {
		cohort, summary := tableCell(c.Cohort), tableCell(c.Summary)
		if cohort == "" || summary == "" {
			continue
		}
		rows = append(rows, fmt.Sprintf("| %s | %s |", cohort, summary))
	}
	if len(rows) > 0 {
		sections = append(sections,
			"## Changes\n\n| Area | Summary |\n| --- | --- |\n"+strings.Join(rows, "\n"))
	}

	var diagrams []string
	for _, d := range a.Diagrams {
		if len(diagrams) == maxDiagrams {
			break
		}
		if validMermaid(d) {
			diagrams = append(diagrams, "```mermaid\n"+strings.TrimSpace(d)+"\n```")
		}
	}
	if len(diagrams) > 0 {
		sections = append(sections, "## Sequence diagram\n\n"+strings.Join(diagrams, "\n\n"))
	}

	if len(sections) == 0 {
		return ""
	}
	body := strings.Join(sections, "\n\n")
	note := "_Generated description — verify against the diff._"
	return BeginMarker + "\n" + body + "\n\n" + note + "\n" + EndMarker
}
