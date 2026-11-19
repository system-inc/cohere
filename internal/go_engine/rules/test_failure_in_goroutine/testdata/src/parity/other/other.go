// Package other declares a function that stops a test, for parity to start as a goroutine from another package.
package other

import "testing"

// Stops stops the test it is given.
func Stops(t *testing.T) {
	t.FailNow()
}
