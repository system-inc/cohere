// Package parity holds every shape S1002's judgment branches on, and the near miss of each: the two operators and
// the others, the constant on either side and on both, each kind of constant, each type the other side may have, the
// negations that fold, and fixes across lines and nested in each other.
package parity

import "parity/constants"

func value() bool { return false }

func number() int { return 0 }

// operators are each operator, with the constant on either side.
func operators() {
	var x bool
	_ = x == true  // reported: x
	_ = x == false // reported: !x
	_ = x != true  // reported: !x
	_ = x != false // reported: x
	_ = true == x  // reported: x
	_ = false == x // reported: !x
	_ = true != x  // reported: !x
	_ = false != x // reported: x
	_ = x && true  // not reported: no equality
	_ = x || false // not reported: no equality
}

// Truth and Falsehood are constants of type bool, and Untyped one with no type.
const (
	Truth     bool = true
	Falsehood bool = false
	Untyped        = true
)

// Named is a named boolean type, and Alias a name for bool itself.
type (
	Named bool
	Alias = bool
)

// NamedTruth is a constant of the named type, and AliasTruth one declared with the alias.
const (
	NamedTruth Named = true
	AliasTruth Alias = true
)

// constantKinds are the kinds of constant the comparison may name.
func constantKinds() {
	var x bool
	var named Named
	_ = x == Truth             // reported: x
	_ = x == Falsehood         // reported: !x
	_ = x != Untyped           // reported: !x
	_ = named == NamedTruth    // not reported: a constant of a named type is none
	_ = x == AliasTruth        // reported or not as the alias is materialized, alike on both sides
	_ = named == true          // reported: a variable of a named boolean type is one
	_ = x == (true)            // not reported: the constant is parenthesized
	_ = Truth == true          // reported: true, the left constant is the one compared
	_ = true == Falsehood      // reported: Falsehood
	_ = Truth == Falsehood     // reported: Falsehood, the left constant being true
	_ = true == false          // not reported: the other side is untyped
	_ = 1 < 2 == true          // not reported: the other side is untyped
	_ = constants.On == x      // not reported: a selector is no identifier
	_ = x == constants.Off     // not reported: a selector is no identifier
	_ = constants.Flag == true // reported: constants.Flag
	_ = x == constants.Flag    // not reported: a variable is no constant
	_ = value() == value()     // not reported: no constant
	_ = number() == number()   // not reported: no constant, no boolean
}

// shadowed are comparisons where true and false name the code's own declarations.
func shadowed() {
	var x bool
	{
		true := false
		_ = x == true // not reported: true is a variable here
	}
	{
		const true = false
		_ = x == true // reported: !x, true is an untyped constant whose value is false
	}
	{
		const false Named = true
		var named Named
		_ = named == false // not reported: false is a constant of a named type here
	}
}

// Generic's Boolean is a type parameter whose every type is a bool, and Mixed one of a bool and a named boolean.
func Generic[Boolean ~bool, Mixed bool | Named, Any any](boolean Boolean, mixed Mixed, anything Any, empty any) {
	_ = boolean == true // reported: boolean
	_ = mixed != true   // reported: !mixed
	_ = empty == true   // not reported: an interface is no boolean
	_ = anything
}

// negations are the negations that fold into one or none.
func negations() {
	var y bool
	_ = !y == true    // reported: !y
	_ = !y == false   // reported: y
	_ = !!y == true   // reported: y
	_ = !!y == false  // reported: !y
	_ = !!!y != false // reported: !y
	_ = !(y) == false // reported: (y)
	_ = (!y) == false // reported: !(!y)
	_ = (y) == true   // reported: (y)
	_ = !y == !false  // not reported: !false is no identifier
}

// comparisons are other sides that are comparisons themselves, where both write the same fix.
func comparisons() {
	var a, b int
	var x, y bool
	_ = a < b == true              // reported: a < b
	_ = (a < b) == false           // reported: !(a < b)
	_ = x == y != true             // reported: !x == y, which negates the comparison of two booleans
	_ = !x == y == false           // reported: x == y
	_ = (x == y) == false          // reported: !(x == y)
	_ = x == true == false         // reported: !x == true, and the inner: x
	_ = (x == true) == false       // reported: !(x == true), and the inner: x
	_ = x && a < b == true         // reported: a < b
	_ = x || (a < b) == false      // reported: !(a < b)
	_ = value() && value() == true // reported: value()
}

// lines are fixes of other sides written across lines, with comments among them.
func lines() {
	_ = value() &&
		value() || (value() && // a comment the render leaves out
		value()) == false // reported, across lines
	_ = (number() <
		number()) == true // reported, across lines
}

// places are comparisons wherever an expression may be.
func places() bool {
	var x bool
	if x == true { // reported
	}
	for x != true { // reported
	}
	switch x == false { // reported
	}
	literal := func() bool { return x == true } // reported
	_ = literal
	values := []bool{x != false} // reported
	_ = values
	return x == false // reported
}
