// Package unrecorded holds two findings only the port makes, and records one.
package unrecorded

func gained() {}

func calls() {
	gained() // port finds: call to gained
	gained()
}
