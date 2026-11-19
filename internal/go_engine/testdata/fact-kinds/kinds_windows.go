package kinds

import (
	"runtime"
	"testing"
)

// platformInteger is windows's.
type platformInteger int64

// platformSize is windows's, which doubles base and counts nothing, so it is pure.
func platformSize(base int) int {
	return base * 2
}

// platformName is windows's.
func platformName() string {
	return "windows"
}

// platformExit returns on windows.
func platformExit() {
	println("staying")
}

// broken is windows's error, whose nil pointer platformError returns.
type broken struct{}

// Error says what broke.
func (*broken) Error() string {
	return "broken"
}

// platformError is windows's: a nil *broken, which as an error is never nil.
func platformError() error {
	var failure *broken
	return failure
}

// platformPairs takes any number of values on windows.
func platformPairs(values []any) {
	println(len(values))
}

// platformWork does nothing on windows.
func platformWork() { /* Windows has no work to do here. */ }

// quietTest is windows's stand-in for a test, whose FailNow ends the goroutine without stopping the test.
type quietTest struct{}

// FailNow ends the goroutine, as testing's does, so stopVia's no-return fact is the same in every build and only its
// stopping calls differ.
func (quietTest) FailNow() {
	runtime.Goexit()
}

// platformTest is windows's stand-in for the test.
func platformTest(*testing.T) quietTest {
	return quietTest{}
}
