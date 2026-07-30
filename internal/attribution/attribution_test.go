package attribution

import (
	"strings"
	"testing"
)

func TestCommit(t *testing.T) {
	on := Commit("Closes #3: fix the bug", true)
	if !strings.Contains(on, CoAuthorTrailer) {
		t.Fatalf("trailer missing when on:\n%s", on)
	}
	// The trailer must be its own paragraph, so git reads it as a trailer.
	if !strings.HasSuffix(on, "\n\n"+CoAuthorTrailer) {
		t.Fatalf("trailer not separated by a blank line:\n%q", on)
	}
	if off := Commit("Closes #3: fix the bug", false); off != "Closes #3: fix the bug" {
		t.Fatalf("message changed when off: %q", off)
	}
}

func TestCommitDoesNotDoubleBlankLine(t *testing.T) {
	// A message that already ends in newlines should not produce three blank lines.
	got := Commit("subject\n\n", true)
	if strings.Contains(got, "\n\n\n") {
		t.Fatalf("collapsed trailing newlines expected, got:\n%q", got)
	}
}

func TestPRBody(t *testing.T) {
	on := PRBody("A change from an issue.", true)
	if !strings.Contains(on, PRFooter) {
		t.Fatalf("footer missing when on:\n%s", on)
	}
	if off := PRBody("A change from an issue.", false); off != "A change from an issue." {
		t.Fatalf("body changed when off: %q", off)
	}
}
