// Package panicking_unreasoned holds a call the planted original panics on, and a record with no reason: no comment
// on the line above it, and no doc comment on the function it is in.
package panicking_unreasoned

func panics() {}
func gained() {}

func calls() {
	panics()
	gained() // port finds: call to gained
}
