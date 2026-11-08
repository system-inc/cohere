// Package library declares deprecated objects of every kind, and objects of the same kinds that are not.
package library

// OldFunction is deprecated.
//
// Deprecated: use NewFunction.
func OldFunction() int { return 0 }

// NewFunction is not.
func NewFunction() int { return 0 }

// OldVariable is deprecated.
//
// Deprecated: use NewVariable.
var OldVariable = 1

// NewVariable is not.
var NewVariable = 1

// OldConstant is deprecated.
//
// Deprecated: use NewConstant.
const OldConstant = 1

// NewConstant is not.
const NewConstant = 1

// OldType is deprecated.
//
// Deprecated: use Record.
type OldType struct{}

// Record holds a deprecated field beside one that is not.
type Record struct {
	// OldField is deprecated.
	//
	// Deprecated: use NewField.
	OldField int
	NewField int
}

// OldMethod is deprecated.
//
// Deprecated: use NewMethod.
func (Record) OldMethod() {}

// NewMethod is not.
func (Record) NewMethod() {}

// Embedding embeds Record, promoting its field and methods.
type Embedding struct{ Record }

// Box is generic.
type Box[T any] struct{ Value T }

// OldMethod is deprecated.
//
// Deprecated: use NewMethod.
func (Box[T]) OldMethod() {}

// NewMethod is not.
func (Box[T]) NewMethod() {}

// Interface has a deprecated method.
type Interface interface {
	// OldMethod is deprecated.
	//
	// Deprecated: use NewMethod.
	OldMethod()
	NewMethod()
}
