package s1

import "sync"

// Inner is a value guarded by a lock, on windows alone, so a copy of it copies the lock there.
type Inner struct {
	Value int
	Lock  sync.Mutex
}
