//go:build !windows

package stopping

import (
	// The same imports as windows's file, so the two builds' imports are equal and the body reach leaves stopVia to
	// the facts' reach, which a differing import would reach whole (#bed0mnk).
	_ "runtime"
	"testing"
)

// platformTest is the test itself.
func platformTest(test *testing.T) *testing.T {
	return test
}
