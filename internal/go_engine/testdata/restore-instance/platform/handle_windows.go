package platform

import "sync"

// Handle holds a lock, so a copy of it copies the lock.
type Handle struct{ lock sync.Mutex }
