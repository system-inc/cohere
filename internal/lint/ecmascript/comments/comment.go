package comments

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/text"
)

// Comment is one comment in a file: where it is, what kind it is, and its text.
type Comment struct {
	Range core.TextRange

	// IsBlock distinguishes a slash-star comment from a double-slash one. JSDoc is a block comment
	// whose text opens with an asterisk, so the two rules that care about JSDoc need this to tell
	// the shapes apart.
	IsBlock bool

	// Text is the full source text of the comment, delimiters included. Rules that want the inner
	// content strip the delimiters themselves, because what counts as content differs by rule: the
	// JSDoc rule wants the asterisk-prefixed lines, the line-length rule wants the whole line.
	Text string

	// StartLine, StartColumn, and EndLine are zero-based positions. The run-detecting rule needs
	// them to answer two questions no range can: whether two comments sit on adjacent lines, and
	// whether they start at the same column, which is what separates a stacked passage from a
	// trailing comment that happens to follow another.
	StartLine   int
	StartColumn int
	EndLine     int
}

// IsJsDoc reports whether a block comment is JSDoc: slash-star-star rather than slash-star.
func (comment Comment) IsJsDoc() bool {
	return comment.IsBlock && strings.HasPrefix(comment.Text, "/**")
}

// ContentLines returns a block comment's inner lines with the delimiters and the leading asterisk
// of each line removed, dropping lines that hold nothing else.
//
// This is what "the comment says one thing" means for the JSDoc rules: the asterisks are furniture,
// so a comment that is three lines of furniture around one line of prose is a one-line comment.
func (comment Comment) ContentLines() []string {
	inner := comment.Text
	inner = strings.TrimPrefix(inner, "/**")
	inner = strings.TrimPrefix(inner, "/*")
	inner = strings.TrimSuffix(inner, "*/")

	var lines []string
	for _, line := range strings.Split(inner, "\n") {
		line = text.TrimWhitespace(line)
		line = strings.TrimPrefix(line, "*")
		line = text.TrimWhitespace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
