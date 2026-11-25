//go:build go1.19

// A file tagged Go 1.19 in a module at Go 1.20 has its tag's standard library, older than the module's.
package before

import "time"

// Found by both: the file's standard library is Go 1.19's.
func taggedOlder() {
	<-time.Tick(time.Second)
}
