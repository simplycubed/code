// Package domain holds the core types shared across the system. These are plain
// data types with no behavior that depends on a model or on GitHub, so every
// other package can compile and test against them without a network.
package domain

// Role is a distinct agent role in the loop.
type Role string

const (
	// RoleImplementer edits code and runs the gate.
	RoleImplementer Role = "implementer"
	// RoleReviewer reads a diff and emits a Verdict. It never edits.
	RoleReviewer Role = "reviewer"
	// RoleFixer edits code on an open pull request to address a human's requested
	// changes, then runs the gate. Like the implementer, it may edit; unlike it,
	// its input is review feedback on an existing branch rather than an issue.
	RoleFixer Role = "fixer"
)

// Issue is the unit of work: a GitHub issue the loop acts on.
type Issue struct {
	Repo   string // "owner/name"
	Number int
	Title  string
	Body   string
}

// RunRequest is a single role turn handed to an engine Runner.
type RunRequest struct {
	Role    Role
	Prompt  string
	WorkDir string // working tree the engine may edit
	Model   string // engine-specific model or deployment identifier
	// MaxTurns bounds the engine's own internal agentic turns for this call.
	MaxTurns int
}

// RunResult is what a Runner reports from a turn. A non-nil Err means the engine
// itself failed (crashed, timed out, bad config). It does NOT mean the gate
// failed; gate results are separate and are the normal signal the loop grades.
type RunResult struct {
	Role         Role
	Summary      string
	FilesTouched []string
	Err          error
}

// ReviewFeedback is the human review on an open pull request that the fixer role
// is asked to address. It is gathered from GitHub by the forge. Notes carries
// only actionable feedback on the current head; the forge filters out stale
// feedback (left against an earlier commit) and the agent's own output, so an
// empty Notes means there is nothing new to do.
type ReviewFeedback struct {
	PR      int
	Branch  string // pull-request head branch, the branch the fixer pushes to
	HeadSHA string // current head commit; feedback is actionable only at this SHA
	Title   string // pull-request title, used to resolve the linked issue
	Notes   []ReviewNote
}

// ReviewNote is one piece of human feedback: a review summary or an inline
// comment. Its text is untrusted input (a human wrote it) and is delimited as
// data when handed to the model, never as instructions.
type ReviewNote struct {
	Author string
	File   string // "" for a pull-request-level review summary
	Line   int
	Body   string
}

// HasFeedback reports whether there is any actionable note to address.
func (r ReviewFeedback) HasFeedback() bool { return len(r.Notes) > 0 }

// Severity ranks a reviewer finding.
type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityMajor   Severity = "major"
	SeverityMinor   Severity = "minor"
)

// Finding is one reviewer-identified problem in a diff.
type Finding struct {
	ID       string // stable within a review, e.g. "F001"
	Severity Severity
	File     string
	Line     int
	Problem  string
	Required string // the change the reviewer requires
}

// Verdict is a reviewer's structured judgment of a diff. The wire schema and the
// prompt that produces it are engine-dependent and live in docs/decisions, not
// here; this is only the in-process type the loop's control flow needs.
type Verdict struct {
	Pass     bool
	Summary  string
	Findings []Finding
}

// HasBlocker reports whether any finding is a blocker. A Verdict that claims Pass
// while carrying a blocker is not trusted by the loop.
func (v Verdict) HasBlocker() bool {
	for _, f := range v.Findings {
		if f.Severity == SeverityBlocker {
			return true
		}
	}
	return false
}
