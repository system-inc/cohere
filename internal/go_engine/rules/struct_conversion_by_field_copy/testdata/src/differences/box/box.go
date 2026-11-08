// Package box declares a generic struct whose instance Of[int] its own type checker makes for Instance, and the
// differences package's type checker makes again where it writes the type.
package box

// Of is a generic struct of two fields.
type Of[T any] struct {
	A T
	B T
}

// Instance is a value of Of[int], typed by this package's type checker.
var Instance = Of[int]{A: 1, B: 2}
