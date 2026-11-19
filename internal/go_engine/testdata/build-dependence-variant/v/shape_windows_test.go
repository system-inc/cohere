package v

import "testing"

// TestPrinted is v's only in-package test, which windows alone compiles.
func TestPrinted(test *testing.T) {
	test.Parallel()

	if Printed() == "" {
		test.Error("want Shape printed")
	}
}
