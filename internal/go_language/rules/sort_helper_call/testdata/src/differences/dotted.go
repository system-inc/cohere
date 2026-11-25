package differences

import (
	"sort"
	. "sort"
)

// A dot import's helper is an identifier, no selector, so the original misses it and the calls beside it.
func dotImport(v []int, f []float64) {
	Sort(IntSlice(v)) // port finds: should use sort.Ints(...) instead of sort.Sort(sort.IntSlice(...))

	sort.Sort(Float64Slice(f)) // port finds: should use sort.Float64s(...) instead of sort.Sort(sort.Float64Slice(...))

	Sort(sort.StringSlice(nil)) // port finds: should use sort.Strings(...) instead of sort.Sort(sort.StringSlice(...))
}

// ownInts sorts itself, so a call of sort.Sort on it is permissible.
type ownInts []int

func (values ownInts) Len() int                        { return len(values) }
func (values ownInts) Less(first int, second int) bool { return values[first] < values[second] }
func (values ownInts) Swap(first int, second int) {
	values[first], values[second] = values[second], values[first]
}

// A type of the code's own still makes the function permissible for the port, as for the original, so the
// dot-imported helper beside it is silenced on both sides, and no finding here is the port's alone.
func dotImportBesideOwn(v []int, own ownInts) {
	Sort(IntSlice(v))
	Sort(own)
}
