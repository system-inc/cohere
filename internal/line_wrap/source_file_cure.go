package line_wrap

import (
	"bytes"
	"errors"
	"slices"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rules/consistency_no_long_line"
)

// keptSpan is the kept bytes of one line, from start up to, not including, end, counted from the source's start.
type keptSpan struct {
	start, end int
}

// keptByte is whether a byte keeps its order through a wrap. A wrap breaks lines, lets gofmt lay them out, adds the
// comma a broken list needs, and reflows a comment, which moves its words between `//` lines; every other byte stays
// where it was in the sequence of the file's bytes, so counting only those says which line of the source each one of
// the output came from.
func keptByte(character byte) bool {
	return !strings.ContainsRune(" \t\r\n,/", rune(character))
}

// keptStarts is how many kept bytes come before each line of source, at lineStarts, and before its end, last.
func keptStarts(source []byte, lineStarts []int) []int {
	starts := make([]int, 0, len(lineStarts)+1)
	kept := 0
	next := 0
	for offset, character := range source {
		for next < len(lineStarts) && lineStarts[next] == offset {
			starts = append(starts, kept)
			next++
		}
		if keptByte(character) {
			kept++
		}
	}
	for next < len(lineStarts) {
		starts = append(starts, kept)
		next++
	}
	return append(starts, kept)
}

// errorUnmapped is a wrap whose kept bytes differ from its source's, so no line of its output can be traced to the line
// it came from. Equivalent rejects such an output first; this guards the cure against a wrap that changed more.
var errorUnmapped = errors.New("the wrap's bytes cannot be traced to the source's lines")

// uncuredSpans are the long lines of the source, past CategoryNotReported, that output, a wrap of it, touched and left
// long: a line some of whose bytes sit on an output line wider than the limit, unless the line came through whole and
// unchanged. Each is given by its kept span.
func (sourceFile *sourceFile) uncuredSpans(output []byte) ([]keptSpan, error) {
	var outputKept []byte
	var outputLines []int
	line := 1
	for _, character := range output {
		if character == '\n' {
			line++
		} else if keptByte(character) {
			outputKept = append(outputKept, character)
			outputLines = append(outputLines, line)
		}
	}
	var sourceKept []byte
	for _, character := range sourceFile.source {
		if keptByte(character) {
			sourceKept = append(sourceKept, character)
		}
	}
	if !bytes.Equal(sourceKept, outputKept) {
		return nil, errorUnmapped
	}
	outputText := strings.Split(string(output), "\n")
	var uncured []keptSpan
	for _, long := range sourceFile.longLines() {
		span := keptSpan{start: sourceFile.keptStarts[long-1], end: sourceFile.keptStarts[long]}
		if span.end == span.start || sourceFile.classify(long) == CategoryNotReported {
			continue
		}
		first, last := outputLines[span.start], outputLines[span.end-1]
		if first == last && outputText[first-1] == sourceFile.lineText(long) {
			continue
		}
		for piece := first; piece <= last; piece++ {
			if width(outputText[piece-1]) > consistency_no_long_line.Limit {
				uncured = append(uncured, span)
				break
			}
		}
	}
	return uncured, nil
}

// isUncured is whether a line holds a kept byte of a line Options.uncured leaves alone.
func (sourceFile *sourceFile) isUncured(line int) bool {
	if len(sourceFile.options.uncured) == 0 {
		return false
	}
	span := keptSpan{start: sourceFile.keptStarts[line-1], end: sourceFile.keptStarts[line]}
	return slices.ContainsFunc(sourceFile.options.uncured, func(uncured keptSpan) bool {
		return span.start < uncured.end && uncured.start < span.end
	})
}

// spansUncured is whether a byte range of the source covers any part of a line Options.uncured leaves alone.
func (sourceFile *sourceFile) spansUncured(start, end int) bool {
	if len(sourceFile.options.uncured) == 0 {
		return false
	}
	for line := sourceFile.lineOf(start); line <= sourceFile.lineOf(max(end-1, start)); line++ {
		if sourceFile.isUncured(line) {
			return true
		}
	}
	return false
}
