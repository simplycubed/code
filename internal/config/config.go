// Package config loads the per-repo contract from .github/simplycubed.yml.
//
// The one hard rule: `gate:` is required. A repo with no gate is refused, on
// purpose, because an ungated loop is not safe to run. That rule is the point of
// this package and is what the tests pin.
//
// The parser here handles a small flat "key: value" subset of YAML, which is all
// the current schema needs and keeps the module dependency-free and its build
// hermetic. It is a deliberate bootstrap; a real YAML parser replaces it when the
// schema grows nested sections (per-role models, budgets).
package config

import (
	"errors"
	"os"
	"strings"
)

// DefaultLabelPrefix is used when the config does not set one. Short by choice:
// the bot byline already carries the full brand, so labels do not repeat it.
const DefaultLabelPrefix = "sc"

// ErrNoGate is returned when the config has no gate command.
var ErrNoGate = errors.New("config: `gate:` is required; a repo with no gate is refused")

// Config is the validated per-repo contract.
type Config struct {
	LabelPrefix string
	Gate        string
	Setup       string
	// Attribution controls whether generated commits and pull requests carry a
	// "SimplyCubed Code" marker. On by default; a repo owner turns it off with
	// `attribution: false`.
	Attribution bool
	// PRDescription selects the generated pull-request body style. "" (the
	// default) is the plain one-line body; "rich" adds a generated walkthrough,
	// changes table, and sequence diagram (issue #16). Opt-in until validated.
	PRDescription string
	// Review turns on the automated reviewer: after the gate passes, a
	// read-only reviewer judges the change and its findings go to the fixer
	// before a human ever sees the pull request. Off by default.
	Review bool
}

// Load reads and validates the config file at path.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(b)
}

// Parse parses and validates config bytes.
func Parse(b []byte) (*Config, error) {
	c := &Config{LabelPrefix: DefaultLabelPrefix, Attribution: true}
	for raw := range strings.SplitSeq(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := unquote(strings.TrimSpace(v))
		switch key {
		case "labelPrefix":
			if val != "" {
				c.LabelPrefix = val
			}
		case "gate":
			c.Gate = val
		case "setup":
			c.Setup = val
		case "attribution":
			// Any explicit falsey value disables it; otherwise it stays on.
			switch strings.ToLower(val) {
			case "false", "no", "off", "0":
				c.Attribution = false
			}
		case "review":
			switch strings.ToLower(val) {
			case "true", "yes", "on", "1":
				c.Review = true
			}
		case "prDescription":
			// Only the documented value enables it; anything else is the default
			// plain body, so a typo fails safe rather than half-enabling.
			if strings.ToLower(val) == "rich" {
				c.PRDescription = "rich"
			}
		}
	}
	if strings.TrimSpace(c.Gate) == "" {
		return nil, ErrNoGate
	}
	return c, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
