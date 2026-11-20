//go:build !windows

package kinds

import (
	"os"
	"testing"
)

// platformInteger is every platform's but windows's.
type platformInteger int32

// calls counts platformSize's calls.
var calls int

// platformSize doubles base and counts its calls, so it is not pure.
func platformSize(base int) int {
	calls++
	return base * 2
}

// platformName is every platform's but windows's.
func platformName() string {
	return "other"
}

// platformExit exits.
func platformExit() {
	os.Exit(1)
}

// platformError returns no error at all.
func platformError() error {
	return nil
}

// platformPairs needs its values even.
func platformPairs(values []anything) {
	if len(values)%2 != 0 {
		panic("odd")
	}
}

// platformWork does real work.
func platformWork() {
	println("working")
}

// platformTest is the test itself.
func platformTest(test *testing.T) *testing.T {
	return test
}
