// Package library declares deprecated objects for the differences fixture.
package library

// OldFunction is deprecated.
//
// Deprecated: use another.
func OldFunction() int { return 0 }

// Inner holds a deprecated field.
type Inner struct {
	// Deprecated: use another.
	OldField int
}

// Outer holds an Inner.
type Outer struct{ Inner Inner }

// OldType is deprecated, and its methods may use deprecated objects.
//
// Deprecated: use another.
type OldType struct{}

// OldMethod is deprecated.
//
// Deprecated: use another.
func (OldType) OldMethod() int { return OldFunction() }
