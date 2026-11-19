// Package middle imports bottom, whose functions its readers ask of.
package middle

import "bodiless/bottom"

// Middle passes its slice on to bottom.Slices, so SA5012 asks whether that parameter is marked.
func Middle(values []int) {
	bottom.Slices(values)
}

// Fresh returns a fresh pointer and reaches nothing of bottom, so the nilness of its result is judged in middle alone.
func Fresh() *int {
	return new(int)
}

// quiet is unexported, so objectpath names no path to it, and an answer about it has no record for the store.
func quiet() int {
	return 1
}
