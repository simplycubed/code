package main

import (
	"flag"
	"io"
	"testing"
)

func newTestFlagSet() (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoDir := fs.String("repo-dir", ".", "")
	return fs, repoDir
}

// The documented usage is `run <owner/repo#N> [flags]`. Stdlib parsing stops at
// the first positional, which silently ignored trailing flags: --repo-dir stayed
// "." and the run failed later with a misleading config error.
func TestParseInterleavedHonorsFlagsAfterPositional(t *testing.T) {
	fs, repoDir := newTestFlagSet()
	rest, err := parseInterleaved(fs, []string{"owner/repo#1", "--repo-dir", "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *repoDir != "/x" {
		t.Fatalf("repo-dir = %q, want /x", *repoDir)
	}
	if len(rest) != 1 || rest[0] != "owner/repo#1" {
		t.Fatalf("positionals = %v, want [owner/repo#1]", rest)
	}
}

func TestParseInterleavedHonorsFlagsBeforePositional(t *testing.T) {
	fs, repoDir := newTestFlagSet()
	rest, err := parseInterleaved(fs, []string{"--repo-dir", "/x", "owner/repo#1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if *repoDir != "/x" || len(rest) != 1 || rest[0] != "owner/repo#1" {
		t.Fatalf("repo-dir = %q positionals = %v", *repoDir, rest)
	}
}

func TestParseInterleavedRejectsUnknownFlags(t *testing.T) {
	fs, _ := newTestFlagSet()
	if _, err := parseInterleaved(fs, []string{"owner/repo#1", "--no-such-flag"}); err == nil {
		t.Fatal("expected an error for an unknown flag after the positional")
	}
}
