//go:build !windows

package stopping

import "testing"

// platformTest is the test itself.
func platformTest(test *testing.T) *testing.T {
	return test
}
