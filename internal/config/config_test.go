package config

import (
	"errors"
	"testing"
)

func TestParseDefaultsAndGate(t *testing.T) {
	c, err := Parse([]byte("gate: make check\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.LabelPrefix != DefaultLabelPrefix {
		t.Fatalf("labelPrefix = %q want default %q", c.LabelPrefix, DefaultLabelPrefix)
	}
	if c.Gate != "make check" {
		t.Fatalf("gate = %q", c.Gate)
	}
}

func TestParseRefusesMissingGate(t *testing.T) {
	_, err := Parse([]byte("labelPrefix: sc\n# no gate here\n"))
	if !errors.Is(err, ErrNoGate) {
		t.Fatalf("want ErrNoGate, got %v", err)
	}
}

func TestParseAttributionDefaultsOnAndDisables(t *testing.T) {
	on, err := Parse([]byte("gate: make check\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !on.Attribution {
		t.Fatal("attribution should default to on")
	}
	off, err := Parse([]byte("gate: make check\nattribution: false\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if off.Attribution {
		t.Fatal("attribution: false should disable attribution")
	}
}

func TestParseOverridesAndComments(t *testing.T) {
	in := `
# a comment
labelPrefix: "simplycubed"
gate: 'pnpm check'
setup: pnpm install
`
	c, err := Parse([]byte(in))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.LabelPrefix != "simplycubed" {
		t.Fatalf("labelPrefix = %q", c.LabelPrefix)
	}
	if c.Gate != "pnpm check" || c.Setup != "pnpm install" {
		t.Fatalf("gate=%q setup=%q", c.Gate, c.Setup)
	}
}

func TestParsePRDescription(t *testing.T) {
	// Default: plain body.
	c, err := Parse([]byte("gate: make check\n"))
	if err != nil || c.PRDescription != "" {
		t.Fatalf("default PRDescription = %q err = %v", c.PRDescription, err)
	}
	// Documented value enables it.
	c, err = Parse([]byte("gate: make check\nprDescription: rich\n"))
	if err != nil || c.PRDescription != "rich" {
		t.Fatalf("PRDescription = %q err = %v, want rich", c.PRDescription, err)
	}
	// Anything else fails safe to the plain body.
	c, err = Parse([]byte("gate: make check\nprDescription: fancy\n"))
	if err != nil || c.PRDescription != "" {
		t.Fatalf("unknown value must fail safe, got %q err = %v", c.PRDescription, err)
	}
}

func TestParseEngineSelection(t *testing.T) {
	// Default is codex, so an existing repo's behaviour is unchanged.
	c, err := Parse([]byte("gate: make check\n"))
	if err != nil || c.Engine != "" {
		t.Fatalf("default Engine = %q err = %v", c.Engine, err)
	}
	for in, want := range map[string]string{"claude": "claude", "CODEX": "codex"} {
		c, err := Parse([]byte("gate: make check\nengine: " + in + "\n"))
		if err != nil || c.Engine != want {
			t.Fatalf("engine %q -> %q err = %v", in, c.Engine, err)
		}
	}
	// An unknown engine falls back to the default rather than failing a run
	// later with an unhelpful error.
	c, err = Parse([]byte("gate: make check\nengine: gpt9\n"))
	if err != nil || c.Engine != "" {
		t.Fatalf("unknown engine should fall back, got %q err = %v", c.Engine, err)
	}
}

func TestAppNameIsNormalised(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"acme-code", "acme-code"},
		{"@acme-code", "acme-code"},
		{"acme-code[bot]", "acme-code"},
		{"@acme-code[bot]", "acme-code"},
	} {
		c, err := Parse([]byte("gate: make check\nappName: " + tc.in + "\n"))
		if err != nil {
			t.Fatalf("Parse(%q): %v", tc.in, err)
		}
		// People write the handle they type in a comment, the login they see on
		// a commit, or the bare name. All three mean the same App.
		if c.AppName != tc.want {
			t.Fatalf("appName %q parsed to %q, want %q", tc.in, c.AppName, tc.want)
		}
	}
}
