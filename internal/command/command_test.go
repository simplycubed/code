package command

import (
	"strings"
	"testing"
)

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

func TestAddressedSeparatesAnAskFromAPassingMention(t *testing.T) {
	for body, want := range map[string]bool{
		"@simplycubed-code go":              true,
		"@simplycubed-code":                 true,
		"@simplycubed-code please help me":  true,
		"  @simplycubed-code: address":      true,
		"thanks @simplycubed-code":          false,
		"cc @simplycubed-code go":           false,
		"@simplycubed-code-experimental go": false,
		"":                                  false,
	} {
		if got := Addressed(body); got != want {
			t.Errorf("Addressed(%q) = %v, want %v", body, got, want)
		}
	}
}

func TestMisdirectedPairsAVerbWithItsSurface(t *testing.T) {
	cases := []struct {
		kind   Kind
		onPR   bool
		wrong  bool
		expect string
	}{
		{Go, false, false, ""},                         // go on an issue is the point
		{Address, true, false, ""},                     // address on a PR is the point
		{Go, true, true, "@simplycubed-code address"},  // go on a PR: name the other verb
		{Address, false, true, "@simplycubed-code go"}, // address on an issue: the #98 case
		{Help, true, false, ""},                        // help applies anywhere
		{None, false, false, ""},
	}
	for _, c := range cases {
		if got := Misdirected(c.kind, c.onPR); got != c.wrong {
			t.Errorf("Misdirected(%q, onPR=%v) = %v, want %v", c.kind, c.onPR, got, c.wrong)
		}
		text := MisdirectedText(c.kind, c.onPR)
		if c.expect == "" {
			if text != "" {
				t.Errorf("MisdirectedText(%q, onPR=%v) should be empty, got %q", c.kind, c.onPR, text)
			}
			continue
		}
		if !strings.Contains(text, c.expect) {
			t.Errorf("MisdirectedText(%q, onPR=%v) = %q, want it to name %q", c.kind, c.onPR, text, c.expect)
		}
	}
}

// The unrecognised-comment answer has to carry the vocabulary, or it is just a
// more verbose silence.
func TestUnknownTextNamesTheVocabulary(t *testing.T) {
	for _, want := range []string{"go", "address", "help"} {
		if !strings.Contains(UnknownText, want) {
			t.Errorf("UnknownText should mention %q, got %q", want, UnknownText)
		}
	}
}
