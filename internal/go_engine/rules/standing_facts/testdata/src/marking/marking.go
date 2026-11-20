// Package marking is the build's package the emptyfunction presence test settles: shape is a marker interface, wide
// is none, and each body says which method it selects.
package marking

// shape is a marker: one unexported method, nothing embedded.
type shape interface{ isShape() }

// wide is no marker: it has two methods.
type wide interface {
	first()
	second()
}

// Circle is a variant of shape.
type Circle struct{}

func (Circle) isShape() { /* A marker: membership, not behavior. */ }

// Other has a method of the marker's name that does not implement shape.
type Other struct{}

func (Other) isShape(int) { /* Of the marker's name alone. */ }

// Pair implements wide.
type Pair struct{}

func (Pair) first() { /* Nothing to do. */ }

func (Pair) second() { /* Nothing to do. */ }

// Check selects shape's method.
func Check(value shape) { value.isShape() }

// Odd selects Other's method of the marker's name.
func Odd() { Other{}.isShape(1) }

// Firsts selects a method of wide's, which is no marker.
func Firsts() { Pair{}.first() }

// Count selects nothing.
func Count() int { return 1 }

// part is a marker too, whose name no method takes.
type part interface{ isPart() }

// isPart is a plain function of part's method's name, no method, so the rule never reads it as a marker's.
func isPart() int { return 1 }

// Keeping wide in use.
var _ wide = Pair{}
