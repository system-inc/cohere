// Package before is package after with its two functions the other way round: the push judges Zero first, holds it
// pure, and so holds Caller pure too, through the same instance.
package before

// Zero is pure: a basic parameter, a result, and arithmetic. Its type parameter is unused.
func Zero[T any](value int) int {
	return value + 1
}

// Caller is pure to the push, which judged Zero first.
func Caller() int {
	return Zero[string](1)
}
