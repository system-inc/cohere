// Package older is a module at Go 1.22, where time.Tick still leaks.
package older

import "time"

// Found by both: the module's standard library is older than Go 1.23.
func returns() {
	<-time.Tick(time.Second)
}
