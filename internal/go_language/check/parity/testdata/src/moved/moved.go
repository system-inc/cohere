// Package moved holds a call both sides find in the same words, recorded as moved, for pairs that differ in span or
// fixes there, and for a pair that agrees whole, where the record does not happen.
package moved

func shared() {}

func calls() {
	// Both find the call; where their spans or fixes differ, the port's are ruled right.
	shared() // port moves: call to shared
}
