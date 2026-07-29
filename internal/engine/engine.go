// Package engine is the seam between the loop and a coding-agent model. The loop
// depends only on the Runner interface, never on a concrete model, which is what
// makes the engine pluggable and the whole system testable with a fake.
package engine

import (
	"context"

	"github.com/simplycubed/code/internal/domain"
)

// Runner runs one role turn and reports what it did. Real implementations shell
// out to a coding-agent CLI (Codex against Azure OpenAI first; Claude Code
// planned). Tests use the fake in this package's fake subpackage.
//
// A Runner does not know about gates, PRs, or GitHub. It edits a working tree
// and returns. Grading and side effects belong to the loop.
type Runner interface {
	Run(ctx context.Context, req domain.RunRequest) (domain.RunResult, error)
}
