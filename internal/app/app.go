// Package app composes the pieces into a runnable pipeline: it onboards an
// isolated worktree for an issue and runs the loop against the repo's own gate,
// with the role prompts (and their bounds) wired in. The concrete engine, forge,
// and VCS are injected, so the pipeline is testable without a model or network.
package app

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/simplycubed/code/internal/config"
	"github.com/simplycubed/code/internal/describe"
	"github.com/simplycubed/code/internal/domain"
	"github.com/simplycubed/code/internal/engine"
	"github.com/simplycubed/code/internal/forge"
	"github.com/simplycubed/code/internal/gate"
	"github.com/simplycubed/code/internal/loop"
	"github.com/simplycubed/code/internal/roles"
	"github.com/simplycubed/code/internal/state"
	"github.com/simplycubed/code/internal/worktree"
)

// Deps are the injected collaborators.
type Deps struct {
	Runner    engine.Runner
	Forge     forge.Forge
	VCS       loop.VCS
	Worktrees *worktree.Manager
}

var issueRefRE = regexp.MustCompile(`^([^/\s]+/[^/#\s]+)#(\d+)$`)

// ParseIssueRef parses an "owner/repo#123" reference.
func ParseIssueRef(s string) (domain.Issue, error) {
	m := issueRefRE.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return domain.Issue{}, fmt.Errorf("app: cannot parse issue ref %q (want owner/repo#N)", s)
	}
	n, _ := strconv.Atoi(m[2])
	return domain.Issue{Repo: m[1], Number: n}, nil
}

// StateLabels returns the full set of state labels for a prefix, for configuring
// the forge's one-state-label enforcement.
func StateLabels(prefix string) []string {
	all := state.All()
	out := make([]string, 0, len(all))
	for _, s := range all {
		out = append(out, state.Label(prefix, s))
	}
	return out
}

func promptBuilder(gateCmd string) func(domain.Role, domain.Issue) string {
	return func(role domain.Role, iss domain.Issue) string {
		def := roles.Implementer
		if role == domain.RoleReviewer {
			def = roles.Reviewer
		}
		return roles.Assemble(def, iss, gateCmd)
	}
}

// describeHook returns the loop's Describe hook: one describer turn that writes
// the artifact into the worktree's scratch directory, then read-parse-render.
// Best-effort by contract — any failure returns "", the plain PR body.
func describeHook(r engine.Runner, workDir string) func(context.Context, domain.Issue) string {
	return func(ctx context.Context, iss domain.Issue) string {
		if _, err := r.Run(ctx, domain.RunRequest{
			Role:    domain.RoleDescriber,
			WorkDir: workDir,
			Prompt:  roles.AssembleDescribe(iss, describe.RelPath),
		}); err != nil {
			return ""
		}
		a, err := describe.Load(workDir)
		if err != nil {
			return ""
		}
		return describe.Render(a)
	}
}

var closesRE = regexp.MustCompile(`(?i)closes\s+#(\d+)`)

// resolveIssue extracts the linked issue number from a pull-request title of the
// form "Closes #N: ...", the shape openPR writes. It returns 0 when there is no
// such reference, in which case state labels go on the pull request itself.
func resolveIssue(title string) int {
	m := closesRE.FindStringSubmatch(title)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// AddressPR runs one pass of the fix-on-request loop over an open pull request:
// it reads the human's review feedback, and if there is anything new to address,
// syncs a worktree to the pull request's head, runs the fixer against the repo's
// gate, and pushes the result back to the same branch.
//
// When there is no new feedback it returns OutcomeNoFeedback without touching the
// repo: a clean no-op, not a stall and not an error.
func AddressPR(ctx context.Context, d Deps, cfg *config.Config, repo string, pr int) (loop.Result, error) {
	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}

	fb, err := d.Forge.Feedback(ctx, repo, pr)
	if err != nil {
		return loop.Result{}, fmt.Errorf("app: read feedback: %w", err)
	}
	if !fb.HasFeedback() {
		return loop.Result{Outcome: loop.OutcomeNoFeedback}, nil
	}

	// The worktree is created off the repo's current HEAD (always a valid ref) and
	// then hard-synced to the pull request's pushed head. The starting ref is
	// throwaway: Sync is what makes the tree correct, whether Add created a fresh
	// worktree or returned one a prior run left behind. Editing a stale tree and
	// pushing would clobber the pushed state the reviewer is looking at, so Sync
	// is mandatory, not an optimization.
	wt, err := d.Worktrees.Add(ctx, fb.Branch, "HEAD")
	if err != nil {
		return loop.Result{}, fmt.Errorf("app: prepare worktree: %w", err)
	}
	if d.VCS != nil {
		if err := d.VCS.Sync(ctx, wt, fb.Branch); err != nil {
			return loop.Result{}, fmt.Errorf("app: sync worktree to PR head: %w", err)
		}
	}

	eng := &loop.Engine{
		Runner: d.Runner,
		Gate:   func(ctx context.Context, dir string) gate.Result { return gate.Run(ctx, dir, cfg.Gate) },
		Forge:  d.Forge,
		VCS:    d.VCS,
		Cfg:    loop.Config{WorkDir: wt, Branch: fb.Branch, LabelPrefix: prefix, Attribute: cfg.Attribution},
	}
	return eng.Fix(ctx, loop.FixRequest{
		Repo:   repo,
		PR:     pr,
		Issue:  resolveIssue(fb.Title),
		Branch: fb.Branch,
		Prompt: roles.AssembleFix(fb, cfg.Gate),
	})
}

// ErrUnauthorized is returned when the actor who triggered a run does not have
// write access to the repository. It is a refusal, not a failure: the loop did
// not run, and nothing was changed.
var ErrUnauthorized = errors.New("app: actor is not authorized to run on this repository")

// Authorize reports whether actor may trigger the agent on repo. An empty actor
// means the caller did not supply one (a local run by a human), which is
// allowed: the credential itself is the authorization there.
//
// This lives here rather than in the workflow because it is the security
// question the product answers, and here it is reachable by a test.
func Authorize(ctx context.Context, d Deps, repo, actor string) error {
	if actor == "" {
		return nil
	}
	ok, err := d.Forge.CanWrite(ctx, repo, actor)
	if err != nil {
		return fmt.Errorf("app: check %s access to %s: %w", actor, repo, err)
	}
	if !ok {
		return fmt.Errorf("%w: %s needs write or admin on %s", ErrUnauthorized, actor, repo)
	}
	return nil
}

// Run onboards a worktree for the issue and runs the loop against cfg.Gate. base
// is the ref the worktree branches from (for example "origin/main").
func Run(ctx context.Context, d Deps, cfg *config.Config, iss domain.Issue, base string) (loop.Result, error) {
	prefix := cfg.LabelPrefix
	if prefix == "" {
		prefix = config.DefaultLabelPrefix
	}
	branch := fmt.Sprintf("%s/%d", prefix, iss.Number)

	wt, err := d.Worktrees.Add(ctx, branch, base)
	if err != nil {
		return loop.Result{}, fmt.Errorf("app: prepare worktree: %w", err)
	}

	eng := &loop.Engine{
		Runner: d.Runner,
		Gate:   func(ctx context.Context, dir string) gate.Result { return gate.Run(ctx, dir, cfg.Gate) },
		Forge:  d.Forge,
		VCS:    d.VCS,
		Prompt: promptBuilder(cfg.Gate),
		Cfg:    loop.Config{WorkDir: wt, Branch: branch, LabelPrefix: prefix, Attribute: cfg.Attribution},
	}
	if cfg.PRDescription == "rich" {
		eng.Describe = describeHook(d.Runner, wt)
	}
	return eng.Run(ctx, iss)
}
