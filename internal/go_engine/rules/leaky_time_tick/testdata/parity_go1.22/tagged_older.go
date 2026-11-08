//go:build go1.21

// A file tagged Go 1.21 in a module at Go 1.22 has the module's standard library, the oldest that builds it.
package older

import "time"

// Found by both: the file's standard library is Go 1.22's.
func taggedOlder() {
	<-time.Tick(time.Second)
}
