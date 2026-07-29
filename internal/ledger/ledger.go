// Package ledger records what a loop run did, as append-only JSON lines. It is
// the audit trail and the source for later cost and status reporting. It is
// engine-independent: it records the events it is handed and knows nothing about
// models. In production the lines are written to a file on an orphan branch; in
// tests they are written to a buffer.
package ledger

import (
	"encoding/json"
	"io"
	"time"
)

// Phase names the kind of event.
const (
	PhaseRound  = "round"
	PhaseRunEnd = "run_end"
)

// Event is one recorded moment in a run. Zero-valued fields are omitted so a
// round line and a run-end line stay compact and self-describing.
type Event struct {
	Time    string `json:"time"`
	RunID   string `json:"run_id"`
	Repo    string `json:"repo"`
	Issue   int    `json:"issue"`
	Phase   string `json:"phase"`
	Round   int    `json:"round,omitempty"`
	Gate    string `json:"gate,omitempty"`    // "pass" | "fail"
	Outcome string `json:"outcome,omitempty"` // set on run_end
	Reason  string `json:"reason,omitempty"`
}

// Writer appends events as JSON lines.
type Writer struct {
	w   io.Writer
	now func() time.Time
}

// New builds a Writer over w using the wall clock.
func New(w io.Writer) *Writer { return &Writer{w: w, now: time.Now} }

// NewWithClock builds a Writer with an injectable clock, for deterministic tests.
func NewWithClock(w io.Writer, now func() time.Time) *Writer {
	return &Writer{w: w, now: now}
}

// Append writes one event as a single JSON line. If Time is unset it is stamped
// from the clock in UTC.
func (l *Writer) Append(e Event) error {
	if e.Time == "" {
		e.Time = l.now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = l.w.Write(b)
	return err
}
