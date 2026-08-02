package command

import (
	"strings"
	"testing"
)

func TestParseRecognisesCommands(t *testing.T) {
	for body, want := range map[string]Kind{
		"@acme-code go":               Go,
		"@acme-code run":              Go,
		"@acme-code address":          Address,
		"@acme-code fix":              Address,
		"@acme-code help":             Help,
		"  @acme-code go  ":           Go,
		"@acme-code GO":               Go,
		"@acme-code go, when you can": Go,
		"@acme-code: address":         Address,
		"@acme-code address\nthanks":  Address,
	} {
		if got := Parse(body, "acme-code"); got != want {
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
		"cc @acme-code go",               // mention not at the start
		"> @acme-code go",                // quoting an earlier comment
		"I think @acme-code should go",   // prose
		"@acme-code",                     // no verb
		"@acme-code please do the thing", // unrecognised verb
		"@acme-code-experimental go",     // a different bot
		"@simplycubedcode go",            // near miss
	} {
		if got := Parse(body, "acme-code"); got != None {
			t.Fatalf("Parse(%q) = %q, want None", body, got)
		}
	}
}

func TestAddressedSeparatesAnAskFromAPassingMention(t *testing.T) {
	for body, want := range map[string]bool{
		"@acme-code go":              true,
		"@acme-code":                 true,
		"@acme-code please help me":  true,
		"  @acme-code: address":      true,
		"thanks @acme-code":          false,
		"cc @acme-code go":           false,
		"@acme-code-experimental go": false,
		"":                           false,
	} {
		if got := Addressed(body, "acme-code"); got != want {
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
		{Go, false, false, ""},                  // go on an issue is the point
		{Address, true, false, ""},              // address on a PR is the point
		{Go, true, true, "@acme-code address"},  // go on a PR: name the other verb
		{Address, false, true, "@acme-code go"}, // address on an issue: the #98 case
		{Help, true, false, ""},                 // help applies anywhere
		{None, false, false, ""},
	}
	for _, c := range cases {
		if got := Misdirected(c.kind, c.onPR); got != c.wrong {
			t.Errorf("Misdirected(%q, onPR=%v) = %v, want %v", c.kind, c.onPR, got, c.wrong)
		}
		text := MisdirectedText(c.kind, c.onPR, "acme-code")
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
		if !strings.Contains(UnknownTextFor("acme-code"), want) {
			t.Errorf("the unknown-comment answer should mention %q, got %q", want, UnknownTextFor("acme-code"))
		}
	}
}

// The handle is the adopter's own App, not ours. App names are globally unique,
// so a hardcoded mention matches nothing in anyone else's repository, and
// because ours is public it would render there as a mention of an account they
// never installed.
func TestHandleIsTheAdoptersOwnApp(t *testing.T) {
	if got := Parse("@acme-code go", "acme-code"); got != Go {
		t.Fatalf("Parse with the repo's own App = %q, want Go", got)
	}
	if got := Parse("@simplycubed-code go", "acme-code"); got != None {
		t.Fatalf("a different App's handle = %q, want None: one repo's bot must not answer to another's", got)
	}
	// No configured App means no handle to answer to. Guessing one would answer
	// as the wrong bot, which is worse than not answering.
	if got := Parse("@acme-code go", ""); got != None {
		t.Fatalf("Parse with no appName = %q, want None", got)
	}
	if got := MentionFor("acme-code[bot]"); got != "@acme-code" {
		t.Fatalf("MentionFor = %q; the [bot] suffix is how GitHub renders the login, not how you mention it", got)
	}
}
