// Package unreasoned holds a record with no reason: no comment on the line above it, and no doc comment on the
// function it is in.
package unreasoned

func gained() {}

func calls() {
	gained() // port finds: call to gained
}
