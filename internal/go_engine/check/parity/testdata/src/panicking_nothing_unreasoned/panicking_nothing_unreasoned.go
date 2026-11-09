// Package panicking_nothing_unreasoned holds a call the planted original panics on, and a record that the port finds
// nothing with no reason: no comment on the line above it, and no doc comment on the function it is in.
package panicking_nothing_unreasoned

func panics() {}

func calls() {
	panics() // port finds nothing
}
