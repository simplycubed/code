// Package command parses the comment commands a human addresses to the agent.
//
// A comment body is untrusted: anyone who can comment can write anything in it.
// Parsing is therefore deliberately narrow. Only a comment that STARTS with the
// mention is a command, so quoting an earlier comment cannot re-trigger a run,
// and only a fixed vocabulary is recognised, so text the agent does not
// understand is ignored rather than guessed at.
package command

import "strings"

// Kind is a recognised command.
type Kind string

const (
	// None means the comment was not addressed to the agent, or asked for
	// something it does not offer.
	None Kind = ""
	// Go starts work on an issue, the same as applying the go label.
	Go Kind = "go"
	// Address runs the fixer over the current review feedback.
	Address Kind = "address"
	// Help asks what the agent understands.
	Help Kind = "help"
)

// Mention is the login the agent answers to.
const Mention = "@simplycubed-code"

// Parse reads a comment body and returns the command it carries.
//
// Anything unrecognised is None, including a bare mention with no verb, so a
// passing reference to the bot cannot start a run.
func Parse(body string) Kind {
	rest, ok := strings.CutPrefix(strings.TrimSpace(body), Mention)
	if !ok {
		return None
	}
	// The mention must be a whole word: @simplycubed-code-experimental is not us.
	if rest != "" && !isSeparator(rest[0]) {
		return None
	}
	// A separator directly after the mention ("@bot: address") is punctuation,
	// not the verb.
	rest = strings.TrimLeft(rest, " \t\n\r:,")
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return None
	}
	switch strings.ToLower(strings.Trim(fields[0], ".,:!?")) {
	case "go", "run", "start":
		return Go
	case "address", "fix":
		return Address
	case "help", "commands":
		return Help
	default:
		return None
	}
}

func isSeparator(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', ':', ',':
		return true
	}
	return false
}

// HelpText lists what the agent understands, for a reply to Help.
const HelpText = "I understand these, addressed to me at the start of a comment:\n\n" +
	"- `@simplycubed-code go` on an issue: start work on it, the same as applying the go label.\n" +
	"- `@simplycubed-code address` on a pull request: address the current review feedback.\n" +
	"- `@simplycubed-code help`: this message.\n\n" +
	"I only act on comments from people with write access, and I never merge."
