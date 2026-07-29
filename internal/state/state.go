// Package state models the sc: label lifecycle. Exactly one state label sits on
// an issue at a time; when the bot advances, it sets the new label and removes
// the old one. GitHub has no radio-button labels, so that mutual exclusion is
// enforced here rather than by the platform.
package state

import "fmt"

// State is a point in an issue's lifecycle.
type State string

const (
	// Go is the trigger a human applies. It is the only human-applied state.
	Go State = "go"
	// Queued: accepted, waiting for a slot.
	Queued State = "queued"
	// Working: a role turn is editing.
	Working State = "working"
	// Review: the review-and-fix cycle is running.
	Review State = "review"
	// Blocked is terminal: the loop stopped and a human is needed.
	Blocked State = "blocked"
	// Done is terminal: the pull request merged.
	Done State = "done"
)

// All returns every state in lifecycle order.
func All() []State { return []State{Go, Queued, Working, Review, Blocked, Done} }

// Label renders the namespaced label for a state, e.g. Label("sc", Working) is
// "sc:working".
func Label(prefix string, s State) string { return prefix + ":" + string(s) }

// HumanApplied reports whether a person sets this state. Only Go.
func (s State) HumanApplied() bool { return s == Go }

// Terminal reports whether this state ends the lifecycle.
func (s State) Terminal() bool { return s == Blocked || s == Done }

// transitions lists the allowed next states for each state.
var transitions = map[State][]State{
	Go:      {Queued},
	Queued:  {Working},
	Working: {Review, Blocked},
	Review:  {Working, Blocked, Done}, // back to Working for a fix round, or Blocked, or pass to Done
	Blocked: {},
	Done:    {},
}

// CanTransition reports whether from -> to is an allowed advance.
func CanTransition(from, to State) bool {
	for _, n := range transitions[from] {
		if n == to {
			return true
		}
	}
	return false
}

// Advance validates a transition. On success the caller sets `to` and removes
// `from`; that pairing is what keeps exactly one state label present.
func Advance(from, to State) error {
	if !CanTransition(from, to) {
		return fmt.Errorf("state: illegal transition %s -> %s", from, to)
	}
	return nil
}
