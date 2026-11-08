// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none. The original walks every assignment and binary expression through code.Matches and
// the port walks the same through the inspector, judging the left of an assignment and the left of a binary
// expression, so these shifts nest one in another, assign through several targets, and shift by the largest amounts a
// constant can name, where a reach of the port's own would show as a finding one side makes and the other does not.
package differences

func use(values ...any) {}

// Found by both, twice: oversized shifts inside the amount of an assignment that shifts by no constant.
func nested(small int8, large uint64) {
	small <<= int8(large<<64) << 8
	use(small)
}

// Found by both: a shift by an amount far past the width, within an int64.
func farPast(large uint64) {
	use(large << 9000)
}

// Found by neither: the width exactly less one.
func justUnder(large int64) {
	use(large >> 63)
}
