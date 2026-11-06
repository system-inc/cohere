package javascript

import "strings"

// JavaScript's String.prototype.indexOf(search, from) and lastIndexOf(search, from), which upstream
// calls directly on source text. Offsets are bytes, as everywhere in this package.

// indexOfFrom is text.indexOf(search, from): the first match starting at or after from, or -1. from is
// clamped to the text, as JavaScript clamps it.
func indexOfFrom(text string, search string, from int) int {
	clampedFrom := from
	if clampedFrom < 0 {
		clampedFrom = 0
	}
	if clampedFrom > len(text) {
		clampedFrom = len(text)
	}
	index := strings.Index(text[clampedFrom:], search)
	if index < 0 {
		return -1
	}
	return clampedFrom + index
}

// lastIndexOfFrom is text.lastIndexOf(search, from): the last match starting at or before from, or -1.
// from is clamped to the text, as JavaScript clamps it.
func lastIndexOfFrom(text string, search string, from int) int {
	clampedFrom := from
	if clampedFrom < 0 {
		clampedFrom = 0
	}
	end := clampedFrom + len(search)
	if end > len(text) {
		end = len(text)
	}
	return strings.LastIndex(text[:end], search)
}
