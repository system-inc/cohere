//go:build linux || custom

package parity

import "runtime"

// Not found: with custom set, windows builds this file.
func eitherTag() {
	use(runtime.GOOS == "windows")
}
