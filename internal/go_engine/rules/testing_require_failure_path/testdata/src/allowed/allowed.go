// Package allowed holds tests the rule must leave alone: each has a way to fail, or skips before anything runs.
package allowed

import "testing"

// A file that isn't a _test.go file holds no tests, whatever its functions are called or do.
func TestShapedOutsideATestFile(test *testing.T) {
	test.Log("nothing checked")
}
