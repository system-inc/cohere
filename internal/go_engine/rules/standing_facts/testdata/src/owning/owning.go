// Package owning is the build's package the presence record test settles: each body says what of its own it holds.
package owning

// Loose writes interface{} in a local variable's type.
func Loose() bool {
	var held interface{} = 1
	return held != nil
}

// Literal writes any in a function literal's parameter.
func Literal() bool {
	check := func(value any) bool { return value != nil }
	return check(1)
}

// Plain writes no candidate.
func Plain() int {
	held := 1
	return held
}

// Sealed declares a marker interface of its own.
func Sealed() bool {
	type sealed interface{ seal() }
	var value sealed
	return value == nil
}

// Box has a method of the local marker's name, and one of another.
type Box struct{}

func (Box) seal() { /* Of the local marker's name. */ }

func (Box) open() { /* Of another name. */ }

// Seals selects Box's seal.
func Seals(box Box) { box.seal() }

// Opens selects Box's open.
func Opens(box Box) { box.open() }
