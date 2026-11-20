package stopping

import (
	"runtime"
	"testing"
)

// quietTest is windows's stand-in for a test, whose FailNow ends the goroutine without stopping the test.
type quietTest struct{}

// FailNow ends the goroutine, as testing's does.
func (quietTest) FailNow() {
	runtime.Goexit()
}

// platformTest is windows's stand-in for the test.
func platformTest(*testing.T) quietTest {
	return quietTest{}
}
