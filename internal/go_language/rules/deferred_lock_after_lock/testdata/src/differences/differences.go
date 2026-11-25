// Package differences holds the cases where the port and the original could plausibly disagree, and records no
// difference, since there is none: both read the same intermediate representation. The original names the method in
// its message by looking up the first call's short name again after matching it, where the port reads both names
// from the full name it matched; the two could disagree where the deferred method is another than the first, or the
// first is reached through another form. The original ranges over every instruction of a block but its last, where
// the port stops one short by its index; they could disagree at a block's edges, a lock the last instruction of its
// block or a defer the first of its own.
package differences

import "sync"

var mutex sync.Mutex
var readWrite sync.RWMutex

// Found by both: a read lock deferred after a read lock names RLock and RUnlock.
func readLock() {
	readWrite.RLock()
	defer readWrite.RLock()
}

// Found by both: a lock taken through a method expression and deferred through a selector names Lock and Unlock.
func mixedForms() {
	(*sync.Mutex).Lock(&mutex)
	defer mutex.Lock()
}

// Found by neither: a read lock and a write lock of one mutex share a receiver, and the write lock is no method the
// original knows.
func readThenWrite() {
	readWrite.RLock()
	defer readWrite.Lock()
}

// Found by neither: a write lock and a read lock of one mutex.
func writeThenRead() {
	readWrite.Lock()
	defer readWrite.RLock()
}

// Found by both: the lock the first instruction of its function and the defer the last before its return.
func onlyInstructions(pointer *sync.Mutex) {
	pointer.Lock()
	defer pointer.Lock()
}

// Found by both: the lock the last instruction of a loop's body but one, its defer just before the jump back.
func lastInLoop(rows int, pointer *sync.RWMutex) {
	for row := 0; row < rows; row++ {
		pointer.RLock()
		defer pointer.RLock()
	}
}

// Found by neither: the lock ends its block, and the defer begins the next.
func acrossBlocks(condition bool) {
	mutex.Lock()
	for condition {
		defer mutex.Lock()
	}
}
