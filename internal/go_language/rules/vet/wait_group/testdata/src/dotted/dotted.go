// Package dotted imports sync with a dot, in a package of its own, since a dot import's WaitGroup would collide with
// parity's.
package dotted

import . "sync"

// A dot import of sync names WaitGroup bare.
func dotted() {
	var group WaitGroup
	go func() {
		group.Add(1) // reported
	}()
}
