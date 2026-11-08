package parity

import . "runtime"

// Found: GOOS by name alone, through a dot import.
func dotted() {
	use(GOOS == "linux")
}
