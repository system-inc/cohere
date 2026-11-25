package parity

import "testing"

// TestComparison holds a comparison in a test file, which both leave alone.
func TestComparison(test *testing.T) {
	var x bool
	if x == true { // not reported: a test file
		test.Log("true")
	}
}
