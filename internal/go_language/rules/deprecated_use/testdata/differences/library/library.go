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

// RetiredFunction is deprecated with a paragraph after its notice, so the notice's message ends with its sentence, not
// the space a closing line break becomes, and a record can name it.
//
// Deprecated: use another.
//
// It stays for old callers.
func RetiredFunction() int { return 0 }

var (
	// GroupedOld is deprecated, alone of its group.
	//
	// Deprecated: use another.
	GroupedOld = 1
	// GroupedNew is not.
	GroupedNew = 2
)

// RetiredConstant is deprecated as RetiredFunction is, with a paragraph after its notice.
//
// Deprecated: use another.
//
// It stays for old callers.
const RetiredConstant = 3

// RetiredType is deprecated as RetiredFunction is, with a paragraph after its notice.
//
// Deprecated: use another.
//
// It stays for old callers.
type RetiredType struct{}
