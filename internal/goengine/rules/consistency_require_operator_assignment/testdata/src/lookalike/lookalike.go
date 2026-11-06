package lookalike

import "counter"

type holder struct {
	total int
	count int
}

// The target on the right of an operator that does not commute is a different computation.
func reversedNonCommutative(count int, step int, text string, prefix string) {
	count = step - count
	count = step / count
	count = step % count
	count = step << count
	count = step >> count
	count = step &^ count
	// `+` concatenates strings, so the port keeps it out of the commuting operators for numbers too.
	text = prefix + text
	count = step + count
	_, _ = count, text
}

// A chain groups to the left, so the target is an operand of the inner operation and not of the assignment's.
func chains(count int, step int, ratio float64, scale float64) {
	count = count + step + 1
	count = count - step - 1
	count = count*step + 1
	count = step*count + 1
	ratio = ratio + scale + 0.5
	_, _ = count, ratio
}

// A key that is a call may answer differently the second time, and an arithmetic key is not proved pure.
func impureKeys(values []int, index int, next func() int) {
	values[next()] = values[next()] + 1
	values[index+1] = values[index+1] + 1
	values[len(values)-1] = values[len(values)-1] + 1
}

// Two spellings that name different places.
func differentPlaces(left holder, right holder, values []int, others []int, index int, other int) {
	left.total = right.total + 1
	left.total = left.count + 1
	values[0] = values[1] + 1
	values[index] = values[other] + 1
	values[0] = others[0] + 1
	values[counter.Limit] = values[4] + 1
	_ = left
}

// A key of interface type keeps its dynamic type, so constants of different types are different entries, and a
// string against a number is compared without asking constant.Compare to match them.
func interfaceKeys(byKey map[any]int) {
	byKey[int8(1)] = byKey[int16(1)] + 1
	byKey[1] = byKey[1.0] + 1
	byKey["1"] = byKey[1] + 1
}

// A conversion, a negation, a receive and an explicit dereference against an implicit one are not the same
// reference as written, and the rule does not prove them equal.
func notReferences(count int, pointer *holder, channel chan int) {
	count = int(count) + 1
	count = -count + 1
	count = <-channel - count
	pointer.total = (*pointer).total + 1
	_ = count
}
