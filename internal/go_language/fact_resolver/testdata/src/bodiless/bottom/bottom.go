// Package bottom declares functions without a body, as a package of assembly does, whose stub.s makes it one go build
// takes; cohere never assembles it. Bodied has a body, to tell the two apart.
package bottom

// Basic takes and returns a basic value, as a pure function may.
func Basic(count int) int

// Pointer returns a pointer, whose nilness a reader may ask.
func Pointer() *int

// Slices takes a slice, which a caller may pass its own slice parameter on to.
func Slices(values []int)

// Bodied is pure, by its body.
func Bodied(count int) int {
	return count + 1
}
