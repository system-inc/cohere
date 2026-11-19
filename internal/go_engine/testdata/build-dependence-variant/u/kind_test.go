package u

import "testing"

// TestPrinted is u's in-package test, which every build compiles.
func TestPrinted(test *testing.T) {
	test.Parallel()

	if Printed() == "" {
		test.Error("want Kind printed")
	}
}
