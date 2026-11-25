// Package panicking_nothing holds a call the planted original panics on, where the port rightly finds nothing, and the
// record that says so.
package panicking_nothing

func panics() {}

func calls() {
	// The planted port names no call here, so where the original panics it is right to find nothing.
	panics() // port finds nothing
}
