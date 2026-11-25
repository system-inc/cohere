//go:build 386

package targeted_panicking

// narrow panics the original, and the port alone finds its call to gained, ruled right with the finding in hand.
func narrow() {
	panics()
	gained() // port finds: call to gained
}
