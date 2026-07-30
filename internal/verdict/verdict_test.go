package verdict

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/domain"
)

func TestParseAcceptsAWellFormedVerdict(t *testing.T) {
	v, err := Parse([]byte(`{"pass":false,"summary":"Two problems.","findings":[
		{"id":"F001","severity":"blocker","file":"a.go","line":4,"problem":"nil deref","required":"check the error"},
		{"id":"F002","severity":"MINOR","file":"b.go","problem":"unused import"}]}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v.Pass || len(v.Findings) != 2 {
		t.Fatalf("verdict = %+v", v)
	}
	// Severity is normalised, so a model shouting does not create a new rank.
	if v.Findings[1].Severity != domain.SeverityMinor {
		t.Fatalf("severity = %q, want minor", v.Findings[1].Severity)
	}
	if !v.HasBlocker() {
		t.Fatal("a blocker finding must be reported as such")
	}
}

// A finding the loop cannot rank is one it cannot act on, so an unknown
// severity is rejected rather than quietly downgraded.
func TestParseRejectsMalformedVerdicts(t *testing.T) {
	for name, in := range map[string]string{
		"not json":         `{`,
		"unknown severity": `{"pass":false,"findings":[{"severity":"catastrophic","problem":"x"}]}`,
		"empty severity":   `{"pass":false,"findings":[{"problem":"x"}]}`,
		"no problem":       `{"pass":false,"findings":[{"severity":"major","problem":"  "}]}`,
	} {
		if _, err := Parse([]byte(in)); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

// The reviewer must not be able to wave through a change it has just called
// unmergeable. The pessimistic reading wins.
func TestTrustedRefusesAPassThatCarriesABlocker(t *testing.T) {
	if !Trusted(domain.Verdict{Pass: true}) {
		t.Fatal("a clean pass should be trusted")
	}
	contradictory := domain.Verdict{Pass: true, Findings: []domain.Finding{{Severity: domain.SeverityBlocker, Problem: "x"}}}
	if Trusted(contradictory) {
		t.Fatal("a pass carrying a blocker must not be trusted")
	}
	if Trusted(domain.Verdict{Pass: false}) {
		t.Fatal("a fail is not a pass")
	}
}

func TestRenderProducesFixerReadyNotes(t *testing.T) {
	notes := Render(domain.Verdict{Findings: []domain.Finding{
		{Severity: domain.SeverityBlocker, File: "a.go", Line: 9, Problem: "nil deref", Required: "check the error"},
		{Severity: domain.SeverityMinor, Problem: "typo"},
	}})
	if len(notes) != 2 {
		t.Fatalf("notes = %d, want 2", len(notes))
	}
	if !strings.Contains(notes[0].Body, "blocker") || !strings.Contains(notes[0].Body, "check the error") {
		t.Fatalf("note should carry severity and the required change: %q", notes[0].Body)
	}
	if notes[0].File != "a.go" || notes[0].Line != 9 {
		t.Fatalf("note should keep its location: %+v", notes[0])
	}
}

func TestLoadReadsFromTheScratchDirectory(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, filepath.FromSlash(RelPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"pass":true,"summary":"ok"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := Load(work)
	if err != nil || !v.Pass {
		t.Fatalf("Load() = %+v, %v", v, err)
	}
	// A missing verdict is an error, not an empty pass.
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("a missing verdict must error")
	}
}
