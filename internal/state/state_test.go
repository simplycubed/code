package state

import "testing"

func TestLabelRendering(t *testing.T) {
	if got := Label("sc", Working); got != "sc:working" {
		t.Fatalf("Label = %q want sc:working", got)
	}
	if got := Label("simplycubed", Go); got != "simplycubed:go" {
		t.Fatalf("Label = %q", got)
	}
}

func TestOnlyGoIsHumanApplied(t *testing.T) {
	for _, s := range All() {
		want := s == Go
		if s.HumanApplied() != want {
			t.Fatalf("%s.HumanApplied()=%v want %v", s, s.HumanApplied(), want)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	if !Blocked.Terminal() || !Done.Terminal() {
		t.Fatal("Blocked and Done must be terminal")
	}
	if Working.Terminal() || Review.Terminal() {
		t.Fatal("Working and Review must not be terminal")
	}
}

func TestAdvanceAllowed(t *testing.T) {
	ok := [][2]State{{Go, Queued}, {Queued, Working}, {Working, Review}, {Working, Blocked}, {Review, Working}, {Review, Done}, {Review, Blocked}}
	for _, p := range ok {
		if err := Advance(p[0], p[1]); err != nil {
			t.Fatalf("Advance(%s,%s) unexpectedly rejected: %v", p[0], p[1], err)
		}
	}
}

func TestAdvanceRejected(t *testing.T) {
	bad := [][2]State{{Go, Done}, {Go, Working}, {Working, Done}, {Queued, Review}, {Done, Working}, {Blocked, Working}}
	for _, p := range bad {
		if err := Advance(p[0], p[1]); err == nil {
			t.Fatalf("Advance(%s,%s) should be illegal", p[0], p[1])
		}
	}
}
