// Package dotted imports math/rand into its file's scope, so its functions are called by their names alone.
package dotted

import . "math/rand"

// Found twice: the dot-imported functions, by name alone and in parentheses.
func dotted() (int, int64) {
	return Intn(1), (Int63n)(1)
}
