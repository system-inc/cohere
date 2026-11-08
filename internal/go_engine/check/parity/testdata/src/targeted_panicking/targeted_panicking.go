// Package targeted_panicking holds, in a file for 386 alone, a call the original panics on and one only the port
// finds, recorded. Loaded for 386 the original panics and the record is met, and loaded for any other target, as the
// machine's own, the original runs and the record is not met.
package targeted_panicking

func panics() {}

func gained() {}
