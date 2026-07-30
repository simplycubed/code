// Package verdict carries the reviewer's judgment of a pending change between
// the engine and the loop. It mirrors internal/describe: the reviewer writes one
// JSON file into the .simplycubed scratch directory, which the commit step
// deletes before staging, so a verdict can never land in a pull request.
//
// A missing or malformed verdict is not a pass. The loop treats it as an absent
// judgment, because trusting silence would let the reviewer be skipped by simply
// failing to write anything.
package verdict

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simplycubed/code/internal/domain"
)

// RelPath is where the reviewer writes its verdict, relative to the worktree.
const RelPath = ".simplycubed/verdict.json"

type wireFinding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Problem  string `json:"problem"`
	Required string `json:"required"`
}

type wireVerdict struct {
	Pass     bool          `json:"pass"`
	Summary  string        `json:"summary"`
	Findings []wireFinding `json:"findings"`
}

// Load reads and validates the verdict from the working tree.
func Load(workDir string) (domain.Verdict, error) {
	b, err := os.ReadFile(filepath.Join(workDir, filepath.FromSlash(RelPath)))
	if err != nil {
		return domain.Verdict{}, err
	}
	return Parse(b)
}

// Parse validates verdict JSON and converts it to the domain type. An unknown
// severity is rejected rather than downgraded: a finding the loop cannot rank is
// one it cannot act on correctly.
func Parse(b []byte) (domain.Verdict, error) {
	var w wireVerdict
	if err := json.Unmarshal(b, &w); err != nil {
		return domain.Verdict{}, fmt.Errorf("verdict: parse: %w", err)
	}
	v := domain.Verdict{Pass: w.Pass, Summary: strings.TrimSpace(w.Summary)}
	for i, f := range w.Findings {
		sev := domain.Severity(strings.ToLower(strings.TrimSpace(f.Severity)))
		switch sev {
		case domain.SeverityBlocker, domain.SeverityMajor, domain.SeverityMinor:
		default:
			return domain.Verdict{}, fmt.Errorf("verdict: finding %d has unknown severity %q", i, f.Severity)
		}
		if strings.TrimSpace(f.Problem) == "" {
			return domain.Verdict{}, fmt.Errorf("verdict: finding %d describes no problem", i)
		}
		v.Findings = append(v.Findings, domain.Finding{
			ID: f.ID, Severity: sev, File: f.File, Line: f.Line,
			Problem: strings.TrimSpace(f.Problem), Required: strings.TrimSpace(f.Required),
		})
	}
	return v, nil
}

// Trusted reports whether the verdict may be acted on as a pass. A verdict that
// claims to pass while carrying a blocker is not trusted: the two statements
// contradict each other, and the safe reading is the pessimistic one.
func Trusted(v domain.Verdict) bool { return v.Pass && !v.HasBlocker() }

// Render turns findings into review notes for the fixer, in the same shape a
// human's review arrives in, so the fixer needs no separate input path.
func Render(v domain.Verdict) []domain.ReviewNote {
	notes := make([]domain.ReviewNote, 0, len(v.Findings))
	for _, f := range v.Findings {
		body := fmt.Sprintf("[%s] %s", f.Severity, f.Problem)
		if f.Required != "" {
			body += " Required: " + f.Required
		}
		notes = append(notes, domain.ReviewNote{Author: "reviewer", File: f.File, Line: f.Line, Body: body})
	}
	return notes
}
