package flagged

import "testing"

// check is a test helper whose parameter's type this package chose, judged like any other.
func check(test *testing.T, got any) { // want "as .any."
	test.Helper()
	if got == nil {
		test.Fatal("got nothing")
	}
}

func TestCheck(test *testing.T) {
	test.Parallel()
	check(test, 1)
}
