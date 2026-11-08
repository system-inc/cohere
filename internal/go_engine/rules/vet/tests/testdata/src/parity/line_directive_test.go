package parity

import "testing"

// A //line directive naming a file that is not a test file leaves this one a test file, judged at the positions the
// directive gives.

//line renamed.go:10
func Testdirected(t *testing.T) {}
