package parity

import "testing"

// stopsUnderLineDirective stops the test it is given. It is declared under a //line directive, so the file token's
// Position names for its call is moved.go, beside this one, while the file it was parsed from, which the answer about
// it names and the related entry is placed in, is this one (startsUnderLineDirective, in parity.go, starts it).
//
//line moved.go:10
func stopsUnderLineDirective(t *testing.T) {
	t.FailNow()
}
