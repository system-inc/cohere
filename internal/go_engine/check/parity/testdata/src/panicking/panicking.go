// Package panicking holds a call the planted original panics on, beside calls the port finds, which the original
// loses with the package.
package panicking

func panics() {}
func shared() {}
func gained() {}

// calls holds the planted records, each ruled in this doc comment: the port rightly finds shared and gained, which
// the original loses to its panic.
func calls() {
	panics()
	shared() // port finds: call to shared
	gained() // port finds: call to gained
}
