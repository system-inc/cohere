package main

import (
	"strings"
	"unicode/utf8"

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

// inParagraph is whether the line is plain prose: `// ` and a word, not a list item, a heading or a directive.
func (line commentLine) inParagraph() bool {
	body, isProse := strings.CutPrefix(line.text, "// ")
	if !isProse || body == "" || body[0] == ' ' || body[0] == '\t' {
		return false
	}
	for _, opening := range []string{"- ", "* ", "+ ", "# "} {
		if strings.HasPrefix(body, opening) {
			return false
		}
	}
	return !(body[0] >= '0' && body[0] <= '9' && strings.Contains(body[:min(len(body), 4)], ". "))
}
