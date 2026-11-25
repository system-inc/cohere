// Package unrecorded holds two findings only the port makes, and records one.
package unrecorded

func gained() {}

// calls holds a recorded call to gained, ruled here as rightly found by the port.
func calls() {
	gained() // port finds: call to gained
	gained()
}
