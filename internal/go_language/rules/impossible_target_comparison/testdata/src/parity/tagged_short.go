//go:build linux && !(t1 || t2 || t3 || t4 || t5 || t6 || t7 || t8 || t9 || t10 || t11)

package parity

import "runtime"

// Not found: linux is read first, but evaluating a constraint reads every tag, so the eleven others are counted all
// the same, too many to try.
func shortTags() {
	use(runtime.GOOS == "windows")
}
