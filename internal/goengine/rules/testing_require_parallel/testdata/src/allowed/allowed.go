// Package allowed holds tests the rule must leave alone: each runs in parallel first or says why.
package allowed

import "testing"

// A file that isn't a _test.go file holds no tests, whatever its functions are called or call.
func TestShapedOutsideATestFile(test *testing.T) {
	test.Run("outside", func(subtest *testing.T) {})
}
