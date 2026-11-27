package starters

import "testing"

// A fixture for the -race list's guard (race_gated_test.go): each exported function starts goroutines or skips only
// through an unexported helper, so the guard finds it only by following the package's own calls.

// Start starts a goroutine through spawn.
func Start(work func()) { spawn(work) }

func spawn(work func()) { go work() }

// Needs skips its test through skipFor.
func Needs(test testing.TB) { skipFor(test) }

func skipFor(test testing.TB) { test.Skip("needs what the box lacks") }

// Plain does neither.
func Plain() int { return 1 }
