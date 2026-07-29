// Package gate runs a repository's own gate command and reports the outcome.
// The gate is the loop's grader: exit zero means the change is good. The gate
// also produces a normalized signature so the loop can tell "the same failure
// again" (a stall) from "a different failure" (progress).
package gate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os/exec"
	"regexp"
	"strings"
)

// Result is the outcome of running a gate command.
type Result struct {
	Passed     bool
	ExitCode   int
	OutputTail string // last tailLines lines of combined stdout+stderr
	Signature  string // normalized hash of the tail, for stall detection
}

const tailLines = 40

// Run executes command via "sh -c" in dir and captures the result. It never
// returns an error itself; a failed command is a Result with Passed=false, which
// is the normal signal the loop grades.
func Run(ctx context.Context, dir, command string) Result {
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()

	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1 // failed to start
		}
	}

	tail := lastLines(string(out), tailLines)
	return Result{
		Passed:     err == nil,
		ExitCode:   code,
		OutputTail: tail,
		Signature:  signature(tail),
	}
}

func lastLines(s string, n int) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

var (
	reAnsi = regexp.MustCompile("\x1b\\[[0-9;]*m")
	reTime = regexp.MustCompile(`\d+(\.\d+)?(ms|µs|ns|s)\b`)
	reHex  = regexp.MustCompile(`0x[0-9a-fA-F]+`)
	reNum  = regexp.MustCompile(`\b\d+\b`)
	reWS   = regexp.MustCompile(`[ \t]+`)
)

// signature strips volatile detail (ANSI codes, durations, hex, bare numbers,
// runs of whitespace) so that the same underlying failure hashes to the same
// value across iterations, even when line numbers or timings differ.
func signature(tail string) string {
	s := reAnsi.ReplaceAllString(tail, "")
	s = reTime.ReplaceAllString(s, "<dur>")
	s = reHex.ReplaceAllString(s, "<hex>")
	s = reNum.ReplaceAllString(s, "<n>")
	s = reWS.ReplaceAllString(s, " ")
	s = strings.TrimSpace(s)
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
