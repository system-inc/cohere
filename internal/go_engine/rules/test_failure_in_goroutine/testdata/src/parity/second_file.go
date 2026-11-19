package parity

import "testing"

// stopsInSecondFile stops the test it is given. It is declared here, in a file other than the one that starts it as a
// goroutine (startsAcrossFiles, in parity.go), so that finding's related entry is in this file.
func stopsInSecondFile(t *testing.T) {
	t.FailNow()
}
