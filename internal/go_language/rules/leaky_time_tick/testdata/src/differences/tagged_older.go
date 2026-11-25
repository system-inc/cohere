//go:build go1.21

// A file tagged Go 1.21 in a package with no version of its own, whose standard library is then the tag's.
package differences

import "time"

// Found by both: the file's standard library is Go 1.21's.
func taggedOlder() {
	<-time.Tick(time.Second)
}
