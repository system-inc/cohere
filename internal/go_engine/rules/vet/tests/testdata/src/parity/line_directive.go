package parity

import "testing"

// A //line directive naming a test file does not make this one a test file, which only its own name does.

//line renamed_test.go:1
func Testrenamed(t *testing.T) {}
