// Package side holds a pure function nothing asks the purity of until the test's edit to top does.
package side

// Factor is what top reads before the edit.
const Factor = 3

// Triple returns three times value and touches nothing else, so it is pure.
func Triple(value int) int {
	return value * Factor
}
