// Package panicking_silent holds a call the planted original panics on and records nothing, so a comparison over it
// proves nothing.
package panicking_silent

func panics() {}

func calls() {
	panics()
}
