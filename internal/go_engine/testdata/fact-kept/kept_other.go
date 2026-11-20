//go:build !windows

package kept

// shifts counts platformShift's calls.
var shifts int

// platformBase is every other platform's, and pure.
func platformBase(value int) int {
	return value + 1
}

// platformShift counts its calls, so it is not pure.
func platformShift(value int) int {
	shifts++
	return value + shifts
}
