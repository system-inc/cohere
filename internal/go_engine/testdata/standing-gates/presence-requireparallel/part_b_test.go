//go:build presence_b

package gate

import "testing"

// TestRuns hands subtest to Run by name, in the build tagged presence_b alone.
func TestRuns(t *testing.T) { t.Run("sub", subtest) }
