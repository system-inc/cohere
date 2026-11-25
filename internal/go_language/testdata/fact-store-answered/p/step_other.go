//go:build !windows

package p

// steps counts platformStep's calls.
var steps int

// platformStep counts its calls, so it is not pure.
func platformStep(value int) int {
	steps++
	return value + steps
}
