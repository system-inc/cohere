//go:build !(t1 || t2 || t3 || t4 || t5 || t6 || t7 || t8 || t9 || t10 || t11) && linux

package parity

import "runtime"

// Not found: eleven other tags are read before linux, too many to try every combination of.
func wideTags() {
	use(runtime.GOOS == "windows")
}
