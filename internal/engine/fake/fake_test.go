package fake

import (
	"context"
	"errors"
	"testing"

	"github.com/simplycubed/code/internal/domain"
)

func TestRunnerScriptsStepsInOrder(t *testing.T) {
	applied := 0
	r := New(
		Step{Summary: "first", Apply: func(string) error { applied++; return nil }},
		Step{Summary: "second"},
	)
	ctx := context.Background()

	res, err := r.Run(ctx, domain.RunRequest{Role: domain.RoleImplementer})
	if err != nil || res.Summary != "first" {
		t.Fatalf("step 1: err=%v summary=%q", err, res.Summary)
	}
	if applied != 1 {
		t.Fatalf("Apply was not called; applied=%d", applied)
	}

	res, _ = r.Run(ctx, domain.RunRequest{Role: domain.RoleImplementer})
	if res.Summary != "second" {
		t.Fatalf("step 2: summary=%q", res.Summary)
	}

	// Past the end of the script: a no-op turn, and Calls does not advance.
	res, _ = r.Run(ctx, domain.RunRequest{Role: domain.RoleImplementer})
	if res.Summary != "no-op" {
		t.Fatalf("past end: summary=%q want no-op", res.Summary)
	}
	if r.Calls() != 2 {
		t.Fatalf("Calls()=%d want 2", r.Calls())
	}
}

func TestRunnerReturnsEngineError(t *testing.T) {
	boom := errors.New("engine boom")
	r := New(Step{Err: boom})
	res, err := r.Run(context.Background(), domain.RunRequest{Role: domain.RoleImplementer})
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v want boom", err)
	}
	if res.Err == nil {
		t.Fatal("RunResult.Err should carry the engine error")
	}
}
