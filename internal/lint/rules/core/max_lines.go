package core

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/shim/core"
	"github.com/system-inc/cohere/internal/lint/ecmascript/comments"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// maxLinesMessage renders the finding. Upstream's template is
// `File has too many lines ({{actual}}). Maximum allowed is {{max}}.`, kept first so the two engines
// say the same thing, with why it matters after it.
func maxLinesMessage(actual int, maximum int) rule.Message {
	return rule.Message{
		Id: "exceed",
		Description: fmt.Sprintf("File has too many lines (%d). Maximum allowed is %d. A file this long "+
			"holds more than one concern, and a reader has to hold all of them to change any. Split it "+
			"along a real seam, a concern, a table, a command group, never by line count, and have every "+
			"caller import the new home directly.", actual, maximum),
	}
}

// MaxLinesOptions is upstream's one positional option, an integer or an object.
type MaxLinesOptions struct {
	Maximum        int
	SkipBlankLines bool
	SkipComments   bool
}

// DefaultMaxLinesOptions is upstream's `defaultOptions: [300]`.
func DefaultMaxLinesOptions() MaxLinesOptions {
	return MaxLinesOptions{Maximum: 300}
}

type maxLinesObjectShape struct {
	Max            *int  `json:"max"`
	SkipBlankLines *bool `json:"skipBlankLines"`
	SkipComments   *bool `json:"skipComments"`
}

// DecodeMaxLinesOptions reads `300`, `{max: 300, skipBlankLines, skipComments}`, or nothing, which is
// the default of 300. An object without `max` keeps 300, as upstream's `Object.hasOwn` test does.
func DecodeMaxLinesOptions(raw []byte) (any, error) {
	options := DefaultMaxLinesOptions()
	if len(raw) == 0 {
		return options, nil
	}
	var maximum int
	if err := json.Unmarshal(raw, &maximum); err == nil {
		if maximum < 0 {
			return DefaultMaxLinesOptions(), fmt.Errorf("max-lines takes a maximum of at least 0, got %d", maximum)
		}
		options.Maximum = maximum
		return options, nil
	}
	var object maxLinesObjectShape
	if err := json.Unmarshal(raw, &object); err != nil {
		return DefaultMaxLinesOptions(), err
	}
	if object.Max != nil {
		if *object.Max < 0 {
			return DefaultMaxLinesOptions(), fmt.Errorf("max-lines takes a maximum of at least 0, got %d", *object.Max)
		}
		options.Maximum = *object.Max
	}
	if object.SkipBlankLines != nil {
		options.SkipBlankLines = *object.SkipBlankLines
	}
	if object.SkipComments != nil {
		options.SkipComments = *object.SkipComments
	}
	return options, nil
}

// MaxLines flags a file with more lines than the maximum.
//
//	valid:   a 300-line file, or a 300-line file ending in a newline, under the default of 300
//	invalid: a 301-line file under the default of 300
//
// # How lines are counted
//
// Raw lines, the way an editor numbers them, and the same way cohere's Swift rule max-file-lines
// counts (agreed with @system_cohere_swift, 2026-10-03, so "2,000 lines" means one thing in both
// languages). Every line counts, blank and comment lines included, unless an option skips them. A
// final line break ends the last line rather than starting another, so a 2,000-line file with a
// trailing newline is 2,000, which is also upstream's: it pops the empty line its split leaves after
// a final break. The line breaks are upstream's: \r\n, \r, \n, U+2028 and U+2029.
//
// skipBlankLines drops a line holding only whitespace. skipComments drops a line holding nothing but
// comment, which upstream finds by asking whether a code token shares the comment's first or last
// line; a line touched by a comment and holding no character outside comments is the same set.
//
// # Where a finding points
//
// From the first line past the maximum to the end of the file, upstream's location, so a
// suppression or a reader lands where the excess begins.
var MaxLines = rule.Rule{
	Name: "max-lines",
	Run: func(ctx rule.Context, options any) rule.Listeners {
		if ctx.SourceFile == nil {
			return nil
		}
		settings, hasSettings := rule.OptionsAs[MaxLinesOptions](options)
		if !hasSettings {
			settings = DefaultMaxLinesOptions()
		}

		text := ctx.SourceFile.Text()
		lines := maxLinesSplit(text)

		var commentLines map[int]bool
		if settings.SkipComments {
			commentLines = maxLinesCommentOnlyLines(text, lines, comments.ForFile(ctx))
		}

		counted := []int{}
		for index, line := range lines {
			if settings.SkipBlankLines && maxLinesIsBlank(text[line.start:line.end]) {
				continue
			}
			if commentLines[index] {
				continue
			}
			counted = append(counted, index)
		}
		if len(counted) <= settings.Maximum {
			return nil
		}
		ctx.ReportRange(core.NewTextRange(lines[counted[settings.Maximum]].start, len(text)),
			maxLinesMessage(len(counted), settings.Maximum))
		return nil
	},
}

// maxLinesLine is one line's text, without its line break, as byte offsets into the file.
type maxLinesLine struct {
	start int
	end   int
}

// maxLinesSplit splits text into lines at upstream's line breaks, and drops the empty line a final
// break leaves, as upstream does.
func maxLinesSplit(text string) []maxLinesLine {
	lines := []maxLinesLine{}
	start := 0
	for index := 0; index < len(text); {
		breakLength := 0
		switch {
		case strings.HasPrefix(text[index:], "\r\n"):
			breakLength = 2
		case text[index] == '\r' || text[index] == '\n':
			breakLength = 1
		case strings.HasPrefix(text[index:], "\u2028") || strings.HasPrefix(text[index:], "\u2029"):
			breakLength = len("\u2028")
		}
		if breakLength == 0 {
			index++
			continue
		}
		lines = append(lines, maxLinesLine{start: start, end: index})
		index += breakLength
		start = index
	}
	lines = append(lines, maxLinesLine{start: start, end: len(text)})
	if len(lines) > 1 && lines[len(lines)-1].start == lines[len(lines)-1].end {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// maxLinesCommentOnlyLines returns the indexes of lines a comment touches that hold no character
// outside comments, other than whitespace.
func maxLinesCommentOnlyLines(text string, lines []maxLinesLine, fileComments []comments.Comment) map[int]bool {
	inComment := make([]bool, len(text))
	for _, comment := range fileComments {
		for offset := comment.Range.Pos(); offset < comment.Range.End() && offset < len(text); offset++ {
			if offset >= 0 {
				inComment[offset] = true
			}
		}
	}
	commentOnly := map[int]bool{}
	for index, line := range lines {
		touched := false
		var outside strings.Builder
		for offset := line.start; offset < line.end; offset++ {
			if inComment[offset] {
				touched = true
				continue
			}
			outside.WriteByte(text[offset])
		}
		if touched && maxLinesIsBlank(outside.String()) {
			commentOnly[index] = true
		}
	}
	return commentOnly
}

// maxLinesIsBlank is upstream's `text.trim() === ""`. JavaScript's trim also drops a byte order mark,
// which Go's TrimSpace keeps.
func maxLinesIsBlank(text string) bool {
	return strings.TrimSpace(strings.ReplaceAll(text, "\ufeff", "")) == ""
}
