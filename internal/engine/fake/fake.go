// Package fake is a deterministic engine.Runner for tests. It performs no
// network calls and spends nothing. A test scripts a sequence of Steps; each
// Run consumes the next one. This is the mechanism that lets the entire loop be
// exercised, including its failure paths, with no model involved.
package fake

import (
	"context"

	"github.com/simplycubed/code/internal/domain"
)

// Step is one scripted turn.
type Step struct {
	// Summary is the RunResult summary this step reports.
	Summary string
	// Apply, if set, mutates the working tree (for example, writes a fix that
	// the repo gate would then pass). It receives the request's WorkDir.
	Apply func(workDir string) error
	// Err, if set, simulates an engine-level failure for this turn.
	Err error
}

// Runner returns scripted Steps in order, one per Run. Calls past the end of the
// script return a harmless no-op turn, which models an engine that ran but
// changed nothing.
type Runner struct {
	Steps []Step
	calls int
}

// New builds a fake Runner from an ordered list of steps.
func New(steps ...Step) *Runner { return &Runner{Steps: steps} }

// Run consumes the next scripted step.
func (r *Runner) Run(_ context.Context, req domain.RunRequest) (domain.RunResult, error) {
	if r.calls >= len(r.Steps) {
		return domain.RunResult{Role: req.Role, Summary: "no-op"}, nil
	}
	s := r.Steps[r.calls]
	r.calls++
	if s.Err != nil {
		return domain.RunResult{Role: req.Role, Err: s.Err}, s.Err
	}
	if s.Apply != nil {
		if err := s.Apply(req.WorkDir); err != nil {
			return domain.RunResult{Role: req.Role, Err: err}, err
		}
	}
	return domain.RunResult{Role: req.Role, Summary: s.Summary}, nil
}

// Calls reports how many scripted steps have been consumed.
func (r *Runner) Calls() int { return r.calls }
