//go:build go1.22

package parity

import "sync"

// From Go 1.22 each iteration of a for loop declares its variables afresh, copying the last iteration's values.
// Ranging over an integer arrived beside it, and yields no lock.
func loopsAtGo122(mutex sync.Mutex) {
	for index, loopMutex := 0, (sync.Mutex{}); index < 1; index++ {
		_ = loopMutex.TryLock()
	}
	for loopHolder, loopArray := (holder{}), ([2]sync.Mutex{}); ; {
		_, _ = loopHolder, &loopArray
		break
	}
	for loopMutex := mutex; ; {
		_ = loopMutex.TryLock()
		break
	}
	for index := range 3 {
		_ = index
	}
	for range 3 {
	}
}
