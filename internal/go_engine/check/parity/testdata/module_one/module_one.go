// Package module_one is a fixture module: it imports its own package inner by the module's path, which resolves only
// when the folder loads as a module, and each package holds a call to shared, which both sides find, and module_one a
// call to gained, which the port alone finds.
package module_one

import "example.com/module_one/inner"

func shared() {}

func gained() {}

func calls() {
	inner.Calls()
	shared() // want "call to shared"
	gained() // want "call to gained"
}
