package parity

import "testing"

// A test file's comparisons are judged like any other.
func TestComparison(test *testing.T) {
	number := 1
	if 1 != number {
		test.Fatal("number is not 1")
	}
}
