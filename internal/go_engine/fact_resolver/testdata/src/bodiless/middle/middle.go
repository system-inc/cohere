// Package middle imports bottom, whose functions its readers ask of.
package middle

import "bodiless/bottom"

// Middle passes its slice on to bottom.Slices.
func Middle(values []int) {
	bottom.Slices(values)
}

// odd passes bottom.Slices a slice of one, so SA5012 asks whether that parameter is marked: it asks a mark only where
// a call passes a slice of known odd length.
func odd() {
	bottom.Slices([]int{1})
}

// Fresh returns a fresh pointer and reaches nothing of bottom, so the nilness of its result is judged in middle alone.
func Fresh() *int {
	return new(int)
}

// quiet is unexported, so objectpath names no path to it, and an answer about it has no record for the store.
func quiet() int {
	return 1
}

// Box is generic, so each instance of it has a Value of its own, an instance's method, which no package declares.
type Box[Content any] struct {
	content Content
}

// Value is what box holds.
func (box Box[Content]) Value() Content {
	return box.content
}
