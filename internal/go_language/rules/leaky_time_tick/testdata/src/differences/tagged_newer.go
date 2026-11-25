//go:build go1.23

// A file tagged Go 1.23 in a package with no version of its own, whose standard library is then the tag's.
package differences

import "time"

// Found by neither: the file's standard library is Go 1.23's.
func taggedNewer() {
	<-time.Tick(time.Second)
}
