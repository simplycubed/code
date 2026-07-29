package domain

import "testing"

func TestVerdictHasBlocker(t *testing.T) {
	cases := []struct {
		name string
		v    Verdict
		want bool
	}{
		{"empty", Verdict{Pass: true}, false},
		{"only minor", Verdict{Findings: []Finding{{Severity: SeverityMinor}}}, false},
		{"has blocker", Verdict{Findings: []Finding{{Severity: SeverityMajor}, {Severity: SeverityBlocker}}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.v.HasBlocker(); got != c.want {
				t.Fatalf("HasBlocker()=%v want %v", got, c.want)
			}
		})
	}
}
