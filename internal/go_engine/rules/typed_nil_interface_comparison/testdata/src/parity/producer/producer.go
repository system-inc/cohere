// Package producer holds functions whose results parity compares with nil from another package, so the nilness facts
// the comparisons read cross a package boundary as they do in real code.
package producer

// Failure is a concrete error.
type Failure struct{}

func (*Failure) Error() string { return "failure" }

// TypedNil always returns a typed nil error.
func TypedNil() error {
	println("not trivial")
	var failure *Failure
	return failure
}

// Untyped returns an untyped nil error.
func Untyped() error {
	println("not trivial")
	return nil
}

// Many returns a typed nil error as its twelfth result.
func Many() (int, int, int, int, int, int, int, int, int, int, int, error) {
	println("not trivial")
	var failure *Failure
	return 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, failure
}

// Holder has a method that returns a typed nil error.
type Holder struct{}

// Get always returns a typed nil error.
func (Holder) Get() error {
	println("not trivial")
	var failure *Failure
	return failure
}
