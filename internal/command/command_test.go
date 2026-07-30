package command

import "testing"

func TestParseRecognisesCommands(t *testing.T) {
	for body, want := range map[string]Kind{
		"@simplycubed-code go":               Go,
		"@simplycubed-code run":              Go,
		"@simplycubed-code address":          Address,
		"@simplycubed-code fix":              Address,
		"@simplycubed-code help":             Help,
		"  @simplycubed-code go  ":           Go,
		"@simplycubed-code GO":               Go,
		"@simplycubed-code go, when you can": Go,
		"@simplycubed-code: address":         Address,
		"@simplycubed-code address\nthanks":  Address,
	} {
		if got := Parse(body); got != want {
			t.Fatalf("Parse(%q) = %q, want %q", body, got, want)
		}
	}
}

// Everything here must be ignored. Each case is a way a comment could start a
// run that its author did not intend.
func TestParseIgnoresAnythingNotAddressedToIt(t *testing.T) {
	for _, body := range []string{
		"",
		"looks good to me",
		"cc @simplycubed-code go",               // mention not at the start
		"> @simplycubed-code go",                // quoting an earlier comment
		"I think @simplycubed-code should go",   // prose
		"@simplycubed-code",                     // no verb
		"@simplycubed-code please do the thing", // unrecognised verb
		"@simplycubed-code-experimental go",     // a different bot
		"@simplycubedcode go",                   // near miss
	} {
		if got := Parse(body); got != None {
			t.Fatalf("Parse(%q) = %q, want None", body, got)
		}
	}
}
