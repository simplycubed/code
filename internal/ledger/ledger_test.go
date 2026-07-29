package ledger

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixedClock() func() time.Time {
	t := time.Date(2026, 7, 30, 1, 2, 3, 0, time.UTC)
	return func() time.Time { return t }
}

func TestAppendWritesOneJSONLinePerEvent(t *testing.T) {
	var buf bytes.Buffer
	l := NewWithClock(&buf, fixedClock())

	if err := l.Append(Event{RunID: "r1", Repo: "o/r", Issue: 7, Phase: PhaseRound, Round: 1, Gate: "fail"}); err != nil {
		t.Fatal(err)
	}
	if err := l.Append(Event{RunID: "r1", Repo: "o/r", Issue: 7, Phase: PhaseRunEnd, Outcome: "blocked", Reason: "stalled"}); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines want 2", len(lines))
	}

	var first Event
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("line 1 not valid json: %v", err)
	}
	if first.Round != 1 || first.Gate != "fail" || first.Time == "" {
		t.Fatalf("unexpected first event: %+v", first)
	}

	// run_end line should omit the zero-valued round field.
	if strings.Contains(lines[1], "\"round\"") {
		t.Fatalf("run_end line should omit round: %s", lines[1])
	}
	if !strings.Contains(lines[1], "\"outcome\":\"blocked\"") {
		t.Fatalf("run_end missing outcome: %s", lines[1])
	}
}
