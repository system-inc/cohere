//go:build go1.23

// A file tagged Go 1.23 in a module at Go 1.20 has its tag's standard library.
package before

import "time"

// Found by neither: the file's standard library is Go 1.23's.
func taggedNewer() {
	<-time.Tick(time.Second)
}
