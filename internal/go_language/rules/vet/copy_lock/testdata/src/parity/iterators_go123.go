//go:build go1.23

package parity

import (
	"iter"
	"sync"
)

// From Go 1.23 a range clause may range over a function, and each value it yields is copied into the variables.
func iterators(sequence iter.Seq[sync.Mutex], pairs iter.Seq2[int, holder], pointers iter.Seq[*sync.Mutex]) {
	var target holder
	for mutex := range sequence {
		_ = mutex.TryLock()
	}
	for index, value := range pairs {
		_, _ = index, value
	}
	for _, target = range pairs {
	}
	for pointer := range pointers {
		_ = pointer
	}
	for range sequence {
	}
	yielding := func(yield func(sync.Mutex) bool) {}
	for mutex := range yielding {
		_ = mutex.TryLock()
	}
	for index, loopMutex := 0, (sync.Mutex{}); index < 1; index++ {
		_ = loopMutex.TryLock()
	}
	_ = &target
}
