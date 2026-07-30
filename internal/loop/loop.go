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
	"github.com/simplycubed/code/internal/ledger"
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
	RunID       string // used in ledger events
}

// VCS commits and pushes the agent's changes so a pull request can be opened
// against them. It is a seam so the loop stays testable without a real git repo.
type VCS interface {
	// Commit stages and commits the working-tree changes in dir. It reports
	// whether there was anything to commit; an empty commit is not an error.
	Commit(ctx context.Context, dir, message string) (committed bool, err error)
	// Push pushes branch from dir to the remote.
	Push(ctx context.Context, dir, branch string) error
}

// Engine runs one issue to a terminal outcome.
type Engine struct {
	Runner engine.Runner
	Gate   GateFunc
	Forge  forge.Forge
	// VCS is optional. When set, the loop commits and pushes the changes before
	// opening the pull request. When nil (as in unit tests), that step is skipped.
	VCS VCS
	// Prompt builds the prompt for a role turn. When nil, the loop falls back to
	// the raw issue body. The app wires this to the roles package so the bounds
	// (never edit the gate or tests to pass) travel with every turn.
	Prompt func(role domain.Role, iss domain.Issue) string
	// Ledger is optional. When set, the loop records a line per round and a
	// terminal line per run.
	Ledger *ledger.Writer
	Cfg    Config
}

// promptFor returns the prompt for a role turn, using the configured builder or
// falling back to the issue body.
func (e *Engine) promptFor(role domain.Role, iss domain.Issue) string {
	if e.Prompt != nil {
		return e.Prompt(role, iss)
	}
	return iss.Body
}

// log records an event if a ledger is configured; otherwise it is a no-op.
func (e *Engine) log(iss domain.Issue, ev ledger.Event) {
	if e.Ledger == nil {
		return
	}
	ev.RunID = e.Cfg.RunID
	ev.Repo = iss.Repo
	ev.Issue = iss.Number
	_ = e.Ledger.Append(ev)
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
		// Act: one implementer turn. The prompt comes from the configured builder
		// (the roles package, wired by the app), which carries the bounds; it
		// falls back to the issue body when no builder is set.
		if _, err := e.Runner.Run(ctx, domain.RunRequest{
			Role:    domain.RoleImplementer,
			WorkDir: e.Cfg.WorkDir,
			Prompt:  e.promptFor(domain.RoleImplementer, iss),
		}); err != nil {
			return e.escalate(ctx, iss, prefix, round, fmt.Sprintf("engine error: %v", err))
		}

		// Grade against the repo's own gate.
		res := e.Gate(ctx, e.Cfg.WorkDir)
		gateStr := "fail"
		if res.Passed {
			gateStr = "pass"
		}
		e.log(iss, ledger.Event{Phase: ledger.PhaseRound, Round: round, Gate: gateStr})
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
	// Persist the agent's changes before opening the PR. Without this the PR
	// would have no content. Skipped when no VCS is wired (unit tests).
	if e.VCS != nil {
		committed, err := e.VCS.Commit(ctx, e.Cfg.WorkDir,
			fmt.Sprintf("Closes #%d: %s", iss.Number, iss.Title))
		if err != nil {
			return e.escalate(ctx, iss, prefix, round, "commit failed: "+err.Error())
		}
		if !committed {
			// The gate passed but nothing changed: there is nothing to propose.
			return e.escalate(ctx, iss, prefix, round, "gate passed but the working tree has no changes to propose")
		}
		if err := e.VCS.Push(ctx, e.Cfg.WorkDir, e.Cfg.Branch); err != nil {
			return e.escalate(ctx, iss, prefix, round, "push failed: "+err.Error())
		}
	}

	url, err := e.Forge.OpenPR(ctx, iss.Repo, e.Cfg.Branch,
		fmt.Sprintf("Closes #%d: %s", iss.Number, iss.Title),
		"Automated change from an issue. A human reviews and merges; this loop does not.")
	if err != nil {
		e.log(iss, ledger.Event{Phase: ledger.PhaseRunEnd, Outcome: string(OutcomeBlocked), Reason: "open PR failed"})
		return Result{Outcome: OutcomeBlocked, Rounds: round, Reason: "open PR failed: " + err.Error()}, err
	}
	// PR is open and waiting on a human: the review state.
	_ = e.Forge.SetState(ctx, iss.Repo, iss.Number, state.Label(prefix, state.Review))
	e.log(iss, ledger.Event{Phase: ledger.PhaseRunEnd, Outcome: string(OutcomePROpened)})
	return Result{Outcome: OutcomePROpened, Rounds: round, PRURL: url}, nil
}

func (e *Engine) escalate(ctx context.Context, iss domain.Issue, prefix string, round int, reason string) (Result, error) {
	_ = e.Forge.Comment(ctx, iss.Repo, iss.Number, "Blocked: "+reason)
	_ = e.Forge.SetState(ctx, iss.Repo, iss.Number, state.Label(prefix, state.Blocked))
	e.log(iss, ledger.Event{Phase: ledger.PhaseRunEnd, Outcome: string(OutcomeBlocked), Reason: reason})
	return Result{Outcome: OutcomeBlocked, Rounds: round, Reason: reason}, nil
}
