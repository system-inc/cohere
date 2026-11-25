// Package renamed imports time under another name beside its own, so time names the import where the select stands,
// and the fix the original writes, time.Sleep, compiles.
package renamed

import (
	"time"
	clock "time"
)

// waits calls time.After under the other name, reported and fixed.
func waits() {
	select { // reported, fixed
	case <-clock.After(time.Second):
	}
}
