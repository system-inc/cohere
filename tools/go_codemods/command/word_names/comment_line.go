package main

import (
	"unicode/utf8"

	"github.com/system-inc/cohere/internal/line_wrap"
	"github.com/system-inc/cohere/tools/go_codemods/rename"
)

// commentLine is one `//` comment of a group, as it reads after its edits.
type commentLine struct {
	start, end int
	// indentation is the source before the comment on its line.
	indentation string
	text        string
	edits       []rename.Edit
	edited      bool
}

func (line commentLine) width() int {
	return columns(line.indentation) + utf8.RuneCountInString(line.text)
}

// inParagraph is whether the line is plain prose a reflow may refill, judged by line_wrap's own test so the
// respelling's reflow and the line wrap read a paragraph alike: not a list item, a heading, a directive or a marker
// line such as `// Serial:`, which a paragraph written straight above it must never be joined into.
func (line commentLine) inParagraph() bool {
	return line_wrap.ProseComment(line.text)
}
