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
