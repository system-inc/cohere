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
