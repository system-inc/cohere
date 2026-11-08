// Package before is a module at Go 1.20, before a go line was the oldest toolchain that builds the module, where
// time.Tick still leaks.
package before

import "time"

// Found by both: the module's standard library is older than Go 1.23.
func returns() {
	<-time.Tick(time.Second)
}
