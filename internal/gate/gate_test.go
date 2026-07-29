package gate

import (
	"context"
	"strings"
	"testing"
)

func TestRunPass(t *testing.T) {
	r := Run(context.Background(), t.TempDir(), "exit 0")
	if !r.Passed || r.ExitCode != 0 {
		t.Fatalf("want pass/0, got passed=%v code=%d", r.Passed, r.ExitCode)
	}
}

func TestRunFailCapturesCodeAndTail(t *testing.T) {
	r := Run(context.Background(), t.TempDir(), "echo boom; exit 3")
	if r.Passed {
		t.Fatal("want fail")
	}
	if r.ExitCode != 3 {
		t.Fatalf("exit code = %d want 3", r.ExitCode)
	}
	if !strings.Contains(r.OutputTail, "boom") {
		t.Fatalf("tail missing output: %q", r.OutputTail)
	}
}

func TestSignatureStableAcrossVolatileDetail(t *testing.T) {
	// Same failure shape, different timing and line number: same signature.
	a := Run(context.Background(), t.TempDir(), `echo "FAIL auth_test at line 42 (took 8ms)"; exit 1`)
	b := Run(context.Background(), t.TempDir(), `echo "FAIL auth_test at line 91 (took 250ms)"; exit 1`)
	if a.Signature != b.Signature {
		t.Fatalf("signatures differ despite same failure shape: %s vs %s", a.Signature, b.Signature)
	}

	// A genuinely different failure: different signature.
	c := Run(context.Background(), t.TempDir(), `echo "FAIL billing_test: nil pointer"; exit 1`)
	if a.Signature == c.Signature {
		t.Fatal("different failures produced the same signature")
	}
}
