// Package middle imports bottom, whose functions its readers ask of.
package middle

import "bodiless/bottom"

// Middle passes its slice on to bottom.Slices, so SA5012 asks whether that parameter is marked.
func Middle(values []int) {
	bottom.Slices(values)
}
