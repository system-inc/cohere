// Package kept holds a caller whose judgment judges callees of its own package, none of which a reader asks of: the
// resolver must hold an answer of its own for each all the same, as it does for the caller (#z9fze0t). hidden is
// unexported, so it has no objectpath, and the resolver holds its answer by its object all the same. Caller's judgment
// ends at Printed, the first callee that is not pure, so After, called past it, is never judged and has no answer.
package kept

// Fresh is pure.
func Fresh(value int) int {
	return value * 2
}

// hidden is pure, and no other package can name it.
func hidden(value int) int {
	return value - 1
}

// Printed prints, so it is not pure.
func Printed(value int) int {
	println(value)
	return value + 1
}

// After is pure, and called only past Printed.
func After(value int) int {
	return value * 3
}

// Caller calls each of the four in turn, so judging it judges them as far as the first that is not pure.
func Caller(value int) int {
	return Fresh(value) + hidden(value) + Printed(value) + After(value)
}
