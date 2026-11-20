package gate

import "testing"

// subtest logs, declared in the test file both builds compile, which no tag changes.
func subtest(t *testing.T) { t.Log("sub") }

// count is handed to no Run.
func count() int { return 1 }
