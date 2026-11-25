package flagged

import "testing"

// A test file is our code as much as any other.
//
//lint:ignore U1000 a helper kept for later // want "^`//lint:ignore` is staticcheck's suppression"
func helper(test *testing.T) { test.Helper() }
