package rule_testing

import "testing"

// The cache this package implements outlives every individual test, so it is swept when this binary
// exits rather than by any one test.
func TestMain(m *testing.M) {
	RunTestsAndCleanUp(m)
}
