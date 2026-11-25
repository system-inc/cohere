package parity

import "time"

// Found by both: the directive below names a test file, and a file is a test file by the name it was parsed from.
//
//line ticks_test.go:1
func underDirective() {
	<-time.Tick(time.Second)
}
