//go:build go1.21

package parity

import "sync"

// Before Go 1.22 a for loop's variables are shared across iterations, so no iteration copies one.
func loopsAtGo121(mutex sync.Mutex) {
	for index, loopMutex := 0, (sync.Mutex{}); index < 1; index++ {
		_ = loopMutex.TryLock()
	}
	for loopHolder := (holder{}); ; {
		_ = loopHolder
		break
	}
	for loopMutex := mutex; ; {
		_ = loopMutex.TryLock()
		break
	}
}
