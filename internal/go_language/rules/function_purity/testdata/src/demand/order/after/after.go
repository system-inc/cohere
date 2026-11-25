// Package after calls an instance of its own pure generic function from a function declared above it. The push judges
// Caller first, in the order the package declares them, meets the instance before it has judged Zero, finds no fact on
// Zero, and holds Caller not pure, which a walk entered at Caller alone would not know.
package after

// Caller is not pure to the push, which reached it before Zero.
func Caller() int {
	return Zero[string](1)
}

// Zero is pure: a basic parameter, a result, and arithmetic. Its type parameter is unused.
func Zero[T any](value int) int {
	return value + 1
}
