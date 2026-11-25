package parity

import ordering "sort"

// A renamed import of package sort calls sort.Sort on a helper all the same, and both find it.
func renamedImport(values []int) {
	ordering.Sort(ordering.IntSlice(values))
}
