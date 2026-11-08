// Package fixes_unwritten holds an upstream-style fixture with no golden file, so a fix to its call has nothing to
// make.
package fixes_unwritten

func shared() {}

func calls() {
	shared() // want "call to shared"
}
