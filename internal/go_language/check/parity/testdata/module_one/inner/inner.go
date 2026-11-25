// Package inner is a package of the fixture module module_one, loaded with it.
package inner

func shared() {}

// Calls calls shared.
func Calls() {
	shared() // want "call to shared"
}
