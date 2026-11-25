//go:build !cgo

package native

// Slots stands in for the C macro where cgo is off, so the package type-checks there, and every reader of the
// fixtures that loads them with cgo off can load this one.
const Slots = 4

// Shape stands in for the type that holds the C struct where cgo is off.
type Shape struct {
	Inner struct{ first int32 }
}
