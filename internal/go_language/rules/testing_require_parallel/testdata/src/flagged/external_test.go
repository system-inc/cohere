package flagged_test

import . "testing"

// The rule knows *testing.T by its type, so a dot import of testing is the same test.
func TestDotImported(test *T) { // want `This test runs alone`
	test.Log("serial")
}
