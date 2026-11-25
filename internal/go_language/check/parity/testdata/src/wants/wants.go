// Package wants holds an upstream-style fixture: the port meets every want, and the original, which never finds a
// call to gained, misses one.
package wants

func shared() {}

func gained() {}

func calls() {
	shared() // want "call to shared"
	gained() // want "call to gained"
}
