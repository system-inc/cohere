//go:build 386

package targeted_adjudicated

func narrow() {
	// Ruled right with the finding in hand: the port alone finds the call to gained.
	gained() // port finds: call to gained
}
