package app_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/simplycubed/code/internal/app"
	forgefake "github.com/simplycubed/code/internal/forge/fake"
)

// Authorization decides who may start a run. It used to live in workflow shell
// where nothing could exercise it; these are the cases that matter.
func TestAuthorize(t *testing.T) {
	ctx := context.Background()

	t.Run("allows an actor with write access", func(t *testing.T) {
		f := &forgefake.Forge{Writers: map[string]bool{"maintainer": true}}
		if err := app.Authorize(ctx, app.Deps{Forge: f}, "o/r", "maintainer"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.CanWriteCalls) != 1 || f.CanWriteCalls[0] != "maintainer" {
			t.Fatalf("expected the actor to be checked, got %v", f.CanWriteCalls)
		}
	})

	// A stranger can open an issue on a public repository. They must not be able
	// to start a run on it.
	t.Run("refuses an actor without write access", func(t *testing.T) {
		f := &forgefake.Forge{Writers: map[string]bool{}}
		err := app.Authorize(ctx, app.Deps{Forge: f}, "o/r", "stranger")
		if !errors.Is(err, app.ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
		if !strings.Contains(err.Error(), "stranger") {
			t.Fatalf("error should name the actor: %v", err)
		}
	})

	// A local run by a human has no actor to check; the credential is the
	// authorization there.
	t.Run("allows an empty actor", func(t *testing.T) {
		f := &forgefake.Forge{}
		if err := app.Authorize(ctx, app.Deps{Forge: f}, "o/r", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.CanWriteCalls) != 0 {
			t.Fatal("an empty actor must not be looked up")
		}
	})

	// Failing to determine access is not the same as being denied it, but both
	// stop the run.
	t.Run("fails closed when access cannot be determined", func(t *testing.T) {
		f := &forgefake.Forge{CanWriteErr: errors.New("api down")}
		err := app.Authorize(ctx, app.Deps{Forge: f}, "o/r", "maintainer")
		if err == nil {
			t.Fatal("expected an error when the check itself fails")
		}
		if errors.Is(err, app.ErrUnauthorized) {
			t.Fatal("a failed check must be distinguishable from a denial")
		}
	})
}
