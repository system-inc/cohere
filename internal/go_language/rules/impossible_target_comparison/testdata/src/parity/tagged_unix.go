//go:build unix

package parity

import "runtime"

func unixComparisons() {
	use(runtime.GOOS == "windows") // Found.
	use(runtime.GOOS == "darwin")  // Not found: darwin is unix.
	use(runtime.GOOS == "js")      // Found: js is not unix.
	use(runtime.GOOS == "illumos") // Not found.
}
