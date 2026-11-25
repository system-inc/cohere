package parity

import "testing"

// otherFileLiteral is a package-level literal declared in another file from the test that starts it, which the parser
// does not resolve across files, so it is not followed.
var otherFileLiteral = func(t *testing.T) {
	t.Fatal("other file")
}
