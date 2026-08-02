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

// MentionFor renders the handle a comment must open with, for the App the
// adopter installed.
//
// This is per-adopter on purpose. GitHub App names are globally unique, so
// every installation has its own login, and a hardcoded handle would match
// nothing in anyone else's repository. Addressing their real bot is also what
// makes GitHub's autocomplete work: it offers accounts that have access to the
// repository, so a user types "@a" and is offered the bot without needing to
// know its name. A prefix that is not an account cannot do that.
func MentionFor(appName string) string {
	return "@" + strings.TrimSuffix(strings.TrimPrefix(appName, "@"), "[bot]")
}

func Parse(body, appName string) Kind {
	if strings.TrimSpace(appName) == "" {
		return None
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(body), MentionFor(appName))
	if !ok {
		return None
	}
	// The handle must be a whole word: @acme-code-experimental is not @acme-code.
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

// Addressed reports whether a comment was aimed at the agent at all, which is
// the same test Parse applies before looking for a verb.
//
// The distinction matters for the reply: a comment that opens with the mention
// and then says something unrecognised deserves an answer, and a comment that
// merely mentions the agent in passing deserves silence.
func Addressed(body, appName string) bool {
	if strings.TrimSpace(appName) == "" {
		return false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(body), MentionFor(appName))
	if !ok {
		return false
	}
	return rest == "" || isSeparator(rest[0])
}

// Misdirected reports whether a verb was addressed to the wrong surface. The
// vocabulary is small enough that this is a lookup rather than a rule: go opens
// a pull request from an issue, address fixes an existing one.
//
// GitHub numbers issues and pull requests from a single sequence, so the number
// in a comment ref never says which of the two it is. Without this check the
// verb runs anyway and fails somewhere below with whatever the GitHub API says,
// which is how `address` on an issue produced a raw GraphQL error.
func Misdirected(k Kind, onPullRequest bool) bool {
	switch k {
	case Go:
		return onPullRequest
	case Address:
		return !onPullRequest
	default:
		return false
	}
}

// MisdirectedText explains the mismatch and names the verb that does apply.
// Being told the right word is the whole point; "that is not valid here" would
// leave the reader exactly where they started.
func MisdirectedText(k Kind, onPullRequest bool, appName string) string {
	m := MentionFor(appName)
	if k == Go && onPullRequest {
		return "`go` starts work on an issue and opens a pull request, and this is already a pull request.\n\n" +
			"To have the current review feedback addressed on this branch, comment `" + m + " address`."
	}
	if k == Address && !onPullRequest {
		return "`address` reads the human review feedback on a pull request, and this is an issue, so there is no review to read.\n\n" +
			"To start work on it, comment `" + m + " go` or apply the `sc:go` label."
	}
	return ""
}

// UnknownTextFor answers a comment addressed to the agent that carries no verb
// it knows. It names the vocabulary, because the alternative is silence and a
// person guessing a second time.
func UnknownTextFor(appName string) string {
	return "I did not recognise a command in that comment.\n\n" + HelpTextFor(appName)
}

// HelpTextFor lists what the agent understands, addressed to the App this
// repository installed. It has to render the adopter's own handle: help that
// told someone to mention a different account would be worse than no help.
func HelpTextFor(appName string) string {
	m := MentionFor(appName)
	return "I understand these, addressed to me at the start of a comment:\n\n" +
		"- `" + m + " go` on an issue: start work on it, the same as applying the go label.\n" +
		"- `" + m + " address` on a pull request: address the current review feedback.\n" +
		"- `" + m + " help`: this message.\n\n" +
		"I only act on comments from people with write access, and I never merge."
}
