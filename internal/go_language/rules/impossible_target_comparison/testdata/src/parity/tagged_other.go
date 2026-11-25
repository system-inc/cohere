//go:build linux && !custom

package parity

import "runtime"

// Found: no value of custom lets windows build this file.
func otherTag() {
	use(runtime.GOOS == "windows")
}
