// Package fixes holds an upstream-style fixture with a golden file: two calls to shared, each wanted, which the
// golden file holds replaced with calls to kept.
package fixes

func shared() {}

func kept() {}

func calls() {
	shared() // want "call to shared"
	shared() // want "call to shared"
}
