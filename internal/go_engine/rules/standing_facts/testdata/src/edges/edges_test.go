package edges

import "testing"

// TestRuns hands subtest to Run by name, so requireparallel reads subtest's body.
func TestRuns(t *testing.T) { t.Run("sub", subtest) }

// subtest logs.
func subtest(t *testing.T) { t.Log("sub") }

// TestLate hands reachedSub to Run by name, which does not stand, so TestLate leaves by the caller direction.
func TestLate(t *testing.T) { t.Run("late", reachedSub) }

// reachedSub logs.
func reachedSub(t *testing.T) { t.Log("late") }
