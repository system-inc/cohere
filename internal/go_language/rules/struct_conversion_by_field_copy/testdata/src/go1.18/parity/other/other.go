// Package other declares struct types of another package, which a literal in parity converts between only when both
// sides are of this one.
package other

// Pair and Couple are identical structs of exported fields.
type Pair struct {
	A int
	B int
}

// Couple is Pair's twin.
type Couple struct {
	A int
	B int
}

// Generic is a generic struct whose instances this package and others each make.
type Generic[T any] struct {
	A T
	B T
}

// Instance is a value of the instance Generic[int] as this package's type checker makes it.
var Instance = Generic[int]{A: 1, B: 2}

// Value is a Couple other packages copy from by its qualified name.
var Value = Couple{A: 1, B: 2}
