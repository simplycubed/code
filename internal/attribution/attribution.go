// Package attribution stamps the commits and pull requests that SimplyCubed Code
// generates with a marker identifying the tool, mirroring the convention Claude
// Code uses for its own commits. It is a product feature, on by default and
// disableable per repo via `attribution: false` in .github/simplycubed.yml, so a
// repo owner who does not want the marker can turn it off.
//
// Two surfaces: a Co-Authored-By trailer on the commit message (git's own
// trailer convention, which GitHub renders as a co-author), and a footer line on
// the pull-request body.
package attribution

import "strings"

// CoAuthorTrailer is the git trailer appended to generated commit messages. It
// follows git's trailer convention (a "Key: value" line in the final paragraph),
// which GitHub reads as a co-author.
const CoAuthorTrailer = "Co-Authored-By: SimplyCubed Code <noreply@simplycubed.com>"

// PRFooter is the line appended to generated pull-request bodies.
const PRFooter = "🤖 Generated with [SimplyCubed Code](https://github.com/simplycubed/code)"

// Commit returns msg with the co-author trailer appended when on. The trailer is
// separated from the body by a blank line so git parses it as a trailer rather
// than as part of the message body. When off, msg is returned unchanged.
func Commit(msg string, on bool) string {
	if !on {
		return msg
	}
	return strings.TrimRight(msg, "\n") + "\n\n" + CoAuthorTrailer
}

// PRBody returns body with the footer appended when on, separated by a blank
// line. When off, body is returned unchanged.
func PRBody(body string, on bool) string {
	if !on {
		return body
	}
	return strings.TrimRight(body, "\n") + "\n\n" + PRFooter
}
