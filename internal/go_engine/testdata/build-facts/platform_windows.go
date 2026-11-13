package facts

import "os"

// size and code are windows's.
const (
	size = 8
	code = 2
)

// platformFinish ends the process on windows.
func platformFinish() {
	os.Exit(1)
}
