//go:build go1.20

package parity

import "sync"

// A file at Go 1.20 is checked at Go 1.21, the earliest a //go:build line may set, so its loop variables are shared
// across iterations and no iteration copies one. An assignment that copies a lock is judged at every version.
func loopsAtGo120(mutex sync.Mutex) {
	for index, loopMutex := 0, (sync.Mutex{}); index < 1; index++ {
		_ = loopMutex.TryLock()
	}
	for loopMutex := mutex; ; {
		_ = loopMutex.TryLock()
		break
	}
}
