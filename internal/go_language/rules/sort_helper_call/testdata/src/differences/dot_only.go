package differences

import . "sort"

// A file whose one import of package sort is a dot import calls sort.Sort all the same, and the port finds its call
// on a helper, which the original, finding no selector, misses.
func dotImportOnly(v []string) {
	Sort(StringSlice(v)) // port finds: should use sort.Strings(...) instead of sort.Sort(sort.StringSlice(...))
}
