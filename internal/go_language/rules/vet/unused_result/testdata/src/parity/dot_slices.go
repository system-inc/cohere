package parity

import . "slices"

// A dot import of generic functions. maps and slices share names, Clone among them, so each is dot imported in a file
// of its own.
func dotImportedSlices(numbers []int) {
	Contains(numbers, 1)        // reported
	Contains[[]int](numbers, 1) // reported
	Clone(numbers)              // reported, as slices.Clone
	Sort(numbers)               // clean
}
