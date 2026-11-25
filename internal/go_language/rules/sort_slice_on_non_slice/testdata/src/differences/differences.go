// Package differences holds the calls where staticcheck_call takes another path than callcheck, or could: a call
// inside a defer or a go statement, whose syntax is the statement around the call, f(g()) where g returns the value
// and its comparison, and a sort function held in a variable, whose callee is known only once the variable is lifted,
// or not at all when two functions meet. The port and the original find the same here, so the fixture records no
// difference.
package differences

import "sort"

func less(int, int) bool { return false }

func arrayAndLess() ([2]int, func(int, int) bool) {
	return [2]int{}, less
}

func nilAndLess() (any, func(int, int) bool) {
	return nil, less
}

func statements(array [2]int) {
	defer sort.Slice(array, less)
	go sort.SliceStable(nil, less)
	defer func() {
		sort.SliceIsSorted(array, less)
	}()
}

func multipleValues() {
	sort.Slice(arrayAndLess())
	sort.SliceStable(nilAndLess())
}

func lifted(condition bool, array [2]int) {
	sorting := sort.Slice
	if condition {
		sorting = sort.Slice
	}
	sorting(array, less)
	choose := sort.Slice
	if condition {
		choose = sort.SliceStable
	}
	choose(array, less)
}
