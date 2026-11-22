// Package stripped starts a helper with go from a test, and the strip takes the helper's body.
package stripped

import "testing"

// TestStarts starts helper on a goroutine of its own.
func TestStarts(t *testing.T) { go helper(t) }

// helper ends the test from that goroutine, and the strip takes its body.
func helper(t *testing.T) { t.Fatal("stopped") }
