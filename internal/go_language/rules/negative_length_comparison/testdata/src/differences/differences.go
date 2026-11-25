// Package differences holds the comparisons where the port and the original could most plausibly part, and records no
// difference, since they part nowhere: a len that is a constant, a builtin in parentheses, a zero of an unusual
// spelling, both orders in one condition, and an operand of a type parameter or a pointer to an array.
package differences

func plausible[Text ~string](text Text, values []int, pointer *[4]int) {
	const fixed = len("constant") < 0
	_ = fixed
	_ = (cap)(values) < -(0)
	_ = 0 > len(values) && len(values) < 0
	_ = len(text) < 0x_0
	_ = len(pointer) < 0
	_ = cap(pointer) < 0
	_ = len([]int{1, 2}) < 0
}
