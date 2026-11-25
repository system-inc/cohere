package line_wrap

import (
	"bytes"
	"errors"
	"go/parser"
	"go/token"
)

// errorCommentsCounted is a wrap whose output holds a different number of comments than its source, so no comment of
// its output can be traced to the one it came from. A wrap only breaks lines, so this guards Options.KeepComments
// against a wrap that did more.
var errorCommentsCounted = errors.New("the wrap's comments cannot be traced to the source's")

// commentPlace is where a comment sits on its line, which is what a line-based reader of it sees besides its text.
type commentPlace struct {
	text string

	// alone is whether only indentation comes before the comment on its line.
	alone bool

	// indentation is what comes before a comment alone on its line, empty for one after code.
	indentation string

	// ends is whether only spaces and tabs come after the comment on its last line.
	ends bool
}

// placeOf is where the comment from start to end of source sits, with text its text.
func placeOf(source []byte, start, end int, text string) commentPlace {
	lineStart := bytes.LastIndexByte(source[:start], '\n') + 1
	before := source[lineStart:start]
	after := source[end:]
	if newline := bytes.IndexByte(after, '\n'); newline >= 0 {
		after = after[:newline]
	}
	place := commentPlace{
		text:  text,
		alone: len(bytes.Trim(before, " \t")) == 0,
		ends:  len(bytes.Trim(after, " \t\r")) == 0,
	}
	if place.alone {
		place.indentation = string(before)
	}
	return place
}

// movedComments are the lines of the source, by their kept spans (see keptByte), holding a comment output, a wrap of
// the source, does not keep as it was: its text, whether it is alone on its line and with what indentation, and
// whether anything follows it on its line. A wrap left to break a construct around one of those lines would change
// what a line-based reader of the comment sees.
func (sourceFile *sourceFile) movedComments(output []byte) ([]keptSpan, error) {
	fileSet := token.NewFileSet()
	file, parseError := parser.ParseFile(fileSet, "", output, parser.ParseComments|parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	tokenFile := fileSet.File(file.FileStart)
	var outputPlaces []commentPlace
	for _, group := range file.Comments {
		for _, comment := range group.List {
			outputPlaces = append(outputPlaces, placeOf(output, tokenFile.Offset(comment.Pos()),
				tokenFile.Offset(comment.End()), comment.Text))
		}
	}
	if len(outputPlaces) != len(sourceFile.comments) {
		return nil, errorCommentsCounted
	}
	var moved []keptSpan
	for index, comment := range sourceFile.comments {
		if placeOf(sourceFile.source, comment.start, comment.end, comment.text) == outputPlaces[index] {
			continue
		}
		line := sourceFile.lineOf(comment.start)
		moved = append(moved, keptSpan{start: sourceFile.keptStarts[line-1], end: sourceFile.keptStarts[line]})
	}
	return moved, nil
}
