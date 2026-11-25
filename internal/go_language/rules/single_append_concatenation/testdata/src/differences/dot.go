package differences

import (
	. "values"
	named "values"
)

// A value or an index assigned through a dot import and recalled through the package's name is one object, which
// the pattern compares by a selector's selected name, so both sides report each loop. The variable is another
// package's, declared outside the for statement, which the loop leaves holding the last one, so the port withholds
// the fix of each.
func dotImported(source []int, target []int) []int {
	// The fix would leave values.Value as it was, where the loop leaves the last element in it.
	for _, Value = range source { // port withholds: Replace loop with call to append
		target = append(target, named.Value)
	}
	// The fix would leave values.Index as it was, where the loop leaves the last index in it.
	for Index = range source { // port withholds: Replace loop with call to append
		target = append(target, source[named.Index])
	}
	return target
}
