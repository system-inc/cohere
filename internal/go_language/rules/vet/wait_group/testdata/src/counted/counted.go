// Package counted imports sync and exports a WaitGroup for indirect, which does not.
package counted

import "sync"

// Group is a WaitGroup another package can reach without importing sync.
var Group sync.WaitGroup

// Holder embeds a WaitGroup, so its Add is WaitGroup's.
type Holder struct {
	sync.WaitGroup
}

// Start adds inside the goroutine it starts.
func Start() {
	go func() {
		Group.Add(1) // reported
	}()
}
