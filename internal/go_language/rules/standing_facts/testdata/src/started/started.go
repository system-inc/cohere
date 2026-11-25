// Package started is testing_goroutine's stripped fixture in presence's form: a test, TestStarts, starting a helper
// with go, helper.
package started

import "testing"

// TestStarts starts helper on a goroutine of its own.
func TestStarts(t *testing.T) { go helper(t) }

// helper ends the test from that goroutine, and the strip takes its body.
func helper(t *testing.T) { t.Fatal("stopped") }
