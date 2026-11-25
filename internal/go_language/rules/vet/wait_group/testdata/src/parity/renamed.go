package parity

import synchronize "sync"

// sync imported under another name is still sync.
func renamed(group *synchronize.WaitGroup) {
	go func() {
		group.Add(1) // reported
	}()
}
