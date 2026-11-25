// Package newer is a module at Go 1.23, where an unreferenced ticker is collected.
package newer

import "time"

// Found by neither: the module's standard library is Go 1.23's.
func returns() {
	<-time.Tick(time.Second)
}
