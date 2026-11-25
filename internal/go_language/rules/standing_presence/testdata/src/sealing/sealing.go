// Package sealing declares a marker only inside Sealed's body, which the test strips.
package sealing

// Sealed declares a marker interface of its own.
func Sealed() bool {
	type sealed interface{ seal() }
	var value sealed
	return value == nil
}
