// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: each judges each operation by its own operands, the original on go/ir through
// irutil.Flatten and the port on the engine's own representation through its translation. These are operands that
// reach the call across loops and branches, through a phi or none, where a flattening that stopped short or went too
// far would show here as a finding one side makes and the other does not.
package differences

import "math"

// Found by both: the call compared inside two nested loops and after them, its variable never reassigned, so no
// phi stands between.
func nestedLoops(value float64, rows int, columns int) bool {
	nan := math.NaN()
	result := false
	for row := 0; row < rows; row++ {
		for column := 0; column < columns; column++ {
			if column%2 == 0 {
				result = value < nan
			}
		}
	}
	return result || value == nan
}

// Found by neither: a loop's phi merges the call with a value the loop computes.
func loopReassigned(value float64, count int) bool {
	nan := math.NaN()
	for index := 0; index < count; index++ {
		nan = nan * 2
	}
	return value == nan
}

// Found by both: NaN on both sides of one operation is one finding.
func bothSides() bool {
	return math.NaN() == math.NaN()
}
