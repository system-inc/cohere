//go:build go1.23

// A file tagged Go 1.23 in a module at Go 1.22 can expect Go 1.23's standard library.
package older

import "time"

// Found by neither: the file's standard library is Go 1.23's.
func taggedReturns() {
	<-time.Tick(time.Second)
}
