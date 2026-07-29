// Package loop is the engine: goal -> act -> grade -> repeat. It ends one of two
// ways. The gate goes green, and it opens a pull request for a human to merge.
// Or it stalls, and it escalates: it labels the issue for a human and opens no
// pull request. There is no third ending and no merge.
package loop

import (
	"context"
	"fmt"

	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine"
	"github.com/simplycubed/code/internal/forge"
	"github.com/simplycubed/code/internal/gate"
	"github.com/simplycubed/code/internal/state"
)

// Outcome is how a run ended.
type Outcome string

const (
	// OutcomePROpened: the gate passed and a pull request was opened.
	OutcomePROpened Outcome = "pr_opened"
	// OutcomeBlocked: the loop stopped and a human is needed. No pull request.
	OutcomeBlocked Outcome = "blocked"
)

// GateFunc grades the working tree. It is injected so tests use a scripted gate
// and production passes a closure over gate.Run.
type GateFunc func(ctx context.Context, workDir string) gate.Result

// Config bounds a run.
type Config struct {
	WorkDir     string
	Branch      string
	LabelPrefix string // default "sc"
	MaxRounds   int    // hard cap on act/grade rounds; default 4
}

// Engine runs one issue to a terminal outcome.
type Engine struct {
	Runner engine.Runner
	Gate   GateFunc
	Forge  forge.Forge
	Cfg    Config
}

// Result summarizes a completed run.
type Result struct {
	Outcome Outcome
	Rounds  int
	PRURL   string
	Reason  string // populated when Blocked
}

// Run drives one issue. It never returns a non-nil error for an ordinary gate
// failure; a stall is a Blocked Result, which is a normal terminal outcome. A
// non-nil error means a side effect (opening the PR) itself failed.
func (e *Engine) Run(ctx context.Context, iss domain.Issue) (Result, error) {
	prefix := e.Cfg.LabelPrefix
	if prefix == "" {
		prefix = "sc"
	}
	maxRounds := e.Cfg.MaxRounds
	if maxRounds <= 0 {
		maxRounds = 4
	}

	sameSigReds := 0
	lastSig := ""

	for round := 1; round <= maxRounds; round++ {
		// Act: one implementer turn. The prompt assembly (issue body, plan, gate
		// output, repo map) is engine-dependent and specified in docs/decisions,
		// not baked in here; this passes the issue body as a placeholder.
		if _, err := e.Runner.Run(ctx, domain.RunRequest{
			Role:    domain.RoleImplementer,
			WorkDir: e.Cfg.WorkDir,
			Prompt:  iss.Body,
		}); err != nil {
			return e.escalate(ctx, iss, prefix, round, fmt.Sprintf("engine error: %v", err))
		}

		// Grade against the repo's own gate.
		res := e.Gate(ctx, e.Cfg.WorkDir)
		if res.Passed {
			return e.openPR(ctx, iss, prefix, round)
		}

		// Red. Stall detection: two reds in a row with the same signature means
		// the change is not moving the failure. Stop rather than burn rounds.
		if res.Signature == lastSig {
			sameSigReds++
		} else {
			sameSigReds = 1
			lastSig = res.Signature
		}
		if sameSigReds >= 2 {
			return e.escalate(ctx, iss, prefix, round, "gate failed the same way twice; stalled")
		}
	}

	return e.escalate(ctx, iss, prefix, maxRounds, "gate not green within max rounds")
}

func (e *Engine) openPR(ctx context.Context, iss domain.Issue, prefix string, round int) (Result, error) {
	url, err := e.Forge.OpenPR(ctx, iss.Repo, e.Cfg.Branch,
		fmt.Sprintf("Closes #%d: %s", iss.Number, iss.Title),
		"Automated change from an issue. A human reviews and merges; this loop does not.")
	if err != nil {
		return Result{Outcome: OutcomeBlocked, Rounds: round, Reason: "open PR failed: " + err.Error()}, err
	}
	// PR is open and waiting on a human: the review state.
	_ = e.Forge.SetState(ctx, iss.Repo, iss.Number, state.Label(prefix, state.Review))
	return Result{Outcome: OutcomePROpened, Rounds: round, PRURL: url}, nil
}

func (e *Engine) escalate(ctx context.Context, iss domain.Issue, prefix string, round int, reason string) (Result, error) {
	_ = e.Forge.Comment(ctx, iss.Repo, iss.Number, "Blocked: "+reason)
	_ = e.Forge.SetState(ctx, iss.Repo, iss.Number, state.Label(prefix, state.Blocked))
	return Result{Outcome: OutcomeBlocked, Rounds: round, Reason: reason}, nil
}
