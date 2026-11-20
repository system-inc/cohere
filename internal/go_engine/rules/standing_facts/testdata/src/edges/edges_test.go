package edges

import "testing"

// TestRuns hands subtest to Run by name, so requireparallel reads subtest's body.
func TestRuns(t *testing.T) { t.Run("sub", subtest) }

// subtest logs.
func subtest(t *testing.T) { t.Log("sub") }
