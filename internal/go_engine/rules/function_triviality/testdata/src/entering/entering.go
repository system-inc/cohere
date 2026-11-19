// Package entering holds a walk for each way IsTrivial meets a callee, for the test of the places an answer names.
package entering

import "errors"

var failure = errors.New("failure")

// Chained is trivial through second, which calls third in another file, past leaf, met twice, which calls nothing.
func Chained() error {
	leaf()
	leaf()
	return second()
}

// second calls a function literal in place, which is named by second's own declaration.
func second() error {
	func() { leaf() }()
	return third()
}

func leaf() {}

// Abroad stops at made, which calls another package's function, before the third it would have called next.
func Abroad() error {
	leaf()
	made()
	return third()
}

func made() error { return errors.New("made") }

// RelayedTwice meets relay again, which calls, so the second meeting is refused as work.
func RelayedTwice() {
	relay()
	relay()
}

func relay() { leaf() }

// Alone calls nothing.
func Alone() error { return failure }

// Enclosing calls only its own function literal, which its own declaration encloses, so it names no other.
func Enclosing() error {
	func() {}()
	return failure
}

// Instantiated calls an instance of generic, which is built from generic's declaration and calls generic.
func Instantiated() error {
	return generic[int]()
}

func generic[Value any]() error { return failure }
