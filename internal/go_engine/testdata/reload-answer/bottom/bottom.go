// Package bottom holds a pure function nothing asks the purity of until the test's edit to top does.
package bottom

// Limit is what top reads before the edit.
const Limit = 3

// Double returns twice value and touches nothing else, so it is pure.
func Double(value int) int {
	return value * 2
}
