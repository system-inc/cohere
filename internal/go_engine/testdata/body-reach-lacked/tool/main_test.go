package main

import "testing"

// TestValue calls value.
func TestValue(test *testing.T) {
	test.Parallel()
	if value() != 1 {
		test.Fail()
	}
}
