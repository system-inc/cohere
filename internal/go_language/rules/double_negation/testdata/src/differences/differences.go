// Package differences holds the negations where the port's fixes and the original's could most plausibly print
// differently, and records no difference, since they part nowhere: a negation spread over lines, beside comments and
// inside parentheses, around a composite operand.
package differences

func plausible(first bool, second bool, values []bool) {
	_ = !(!(first &&
		second))
	_ = ! /* inner */ !first
	_ = !!first // after
	_ = !(!values[len(values)-1])
	_ = !!func() bool { return first }()
	_ = !(!first)
}
