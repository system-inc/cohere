package m

import "os"

// platformFinish ends the process on windows.
func platformFinish() {
	os.Exit(1)
}
