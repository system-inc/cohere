package parity

import "runtime"

func plan9Comparisons() {
	use(runtime.GOOS == "plan9")   // Not found.
	use(runtime.GOOS == "unix")    // Not found: unix is a tag, no operating system.
	use(runtime.GOOS == "windows") // Found.
}
