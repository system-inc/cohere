// Package parity holds the shapes SA2003 depends on, each with a comment saying whether the two are expected to
// find it: where the mutex lives, what stands between the lock and the defer, and the lock taken or deferred inside
// goroutines, closures, deferred functions, method values, method expressions and interfaces.
package parity

import "sync"

var global sync.Mutex
var globalReadWrite sync.RWMutex
var other sync.Mutex

// Found by both: a global mutex.
func globalLock() {
	global.Lock()
	defer global.Lock()
}

// Found by both: a global read lock.
func globalReadLock() {
	globalReadWrite.RLock()
	defer globalReadWrite.RLock()
}

// Found by neither: the original knows the read lock of a sync.RWMutex and not its write lock.
func globalWriteLock() {
	globalReadWrite.Lock()
	defer globalReadWrite.Lock()
}

// Found by neither: the lock released as meant.
func released() {
	global.Lock()
	defer global.Unlock()
	globalReadWrite.RLock()
	defer globalReadWrite.RUnlock()
}

// Found by neither: two mutexes.
func twoMutexes() {
	global.Lock()
	defer other.Lock()
}

// Found by both: a local mutex, whose address both calls take from one allocation.
func localLock() {
	var mutex sync.Mutex
	mutex.Lock()
	defer mutex.Lock()
}

// Found by both: a local pointer to a mutex.
func localPointer() {
	mutex := &sync.Mutex{}
	mutex.Lock()
	defer mutex.Lock()
}

// Found by both: a mutex passed in.
func parameter(mutex *sync.Mutex, readWrite *sync.RWMutex) {
	mutex.Lock()
	defer mutex.Lock()
	readWrite.RLock()
	defer readWrite.RLock()
}

type guarded struct {
	mutex   sync.Mutex
	pointer *sync.Mutex
	values  []int
}

// Found by neither: a field of a struct is read anew for each call, two values for one mutex.
func (value *guarded) field() {
	value.mutex.Lock()
	defer value.mutex.Lock()
}

// Found by neither: a pointer field is loaded anew for each call.
func (value *guarded) pointerField() {
	value.pointer.Lock()
	defer value.pointer.Lock()
}

type embedded struct {
	sync.Mutex
	count int
}

// Found by neither: a promoted method reaches the mutex through a field.
func (value *embedded) promoted() {
	value.Lock()
	defer value.Lock()
}

// Found by neither: a call through an interface is no static call.
func throughInterface(locker sync.Locker) {
	locker.Lock()
	defer locker.Lock()
}

// Found by neither: a mutex behind an interface it was stored in.
func storedInInterface() {
	var locker sync.Locker = &global
	locker.Lock()
	defer locker.Lock()
}

// Found by neither: a method value is a closure bound to the mutex, called dynamically.
func methodValue() {
	lock := global.Lock
	lock()
	defer lock()
}

// Found by both: a method expression is the method itself, called with the mutex.
func methodExpression() {
	(*sync.Mutex).Lock(&global)
	defer (*sync.Mutex).Lock(&global)
}

// Found by both: the lock and its defer inside a closure called in place.
func insideClosure() {
	func() {
		global.Lock()
		defer global.Lock()
	}()
}

// Found by both: inside a goroutine.
func insideGoroutine() {
	go func() {
		globalReadWrite.RLock()
		defer globalReadWrite.RLock()
	}()
}

// Found by neither: inside a deferred function, a captured parameter is a variable the closure loads anew for each
// call, two values for one mutex.
func insideDeferred(mutex *sync.Mutex) {
	defer func() {
		mutex.Lock()
		defer mutex.Lock()
	}()
}

// Found by both: a captured local mutex.
func capturedLocal() {
	var mutex sync.Mutex
	run := func() {
		mutex.Lock()
		defer mutex.Lock()
	}
	run()
}

// Found by neither: the lock deferred in a function literal, a defer of the literal and not of Lock.
func deferredLiteral() {
	global.Lock()
	defer func() {
		global.Lock()
	}()
}

// Found by neither: the lock taken in a goroutine is a go statement, not a defer.
func lockInGoroutine() {
	global.Lock()
	go global.Lock()
}

// Found by neither: the lock taken by a deferred call, then deferred again, where the first is no call.
func deferredTwice() {
	defer global.Lock()
	defer global.Lock()
}

// Found by both, once: the defer that follows the lock, and not the one after it.
func twoDefers() {
	global.Lock()
	defer global.Lock()
	defer global.Lock()
}

// Found by both: a constant and a type declared between build no instruction, so the defer is still next.
func declarationsBetween() {
	global.Lock()
	const limit = 4
	type count int
	defer global.Lock()
}

// Found by neither: an assignment to the blank identifier between builds an instruction of its own, even of a
// constant.
func blankBetween() {
	global.Lock()
	_ = 4
	defer global.Lock()
}

// Found by neither: a call between.
func callBetween() {
	global.Lock()
	println("")
	defer global.Lock()
}

// Found by neither: the defer in another block than the lock.
func otherBlock(condition bool) {
	global.Lock()
	if condition {
		defer global.Lock()
	}
}

// Found by both: inside a loop body.
func inLoop(rows int) {
	for row := 0; row < rows; row++ {
		global.Lock()
		defer global.Lock()
	}
}

// Found by both: inside a branch.
func inBranch(condition bool, mutex *sync.Mutex) {
	if condition {
		mutex.Lock()
		defer mutex.Lock()
	}
}

type ownMutex struct{ held bool }

func (mutex *ownMutex) Lock()   { mutex.held = true }
func (mutex *ownMutex) Unlock() { mutex.held = false }

// Found by neither: a Lock that is not sync's.
func ownLock(mutex *ownMutex) {
	mutex.Lock()
	defer mutex.Lock()
}

// Found by neither: a method of a type parameter is called through its constraint.
func generic[Locker sync.Locker](locker Locker) {
	locker.Lock()
	defer locker.Lock()
}

// Found by neither: try lock is no lock the original knows.
func tryLock() {
	if global.TryLock() {
		defer global.Lock()
	}
}

// Found by neither: the lock is the last instruction of its block.
func lockLast() {
	global.Lock()
}

// Found by neither: a write lock deferred after a read lock, a deferred lock the original does not know.
func readThenWrite() {
	globalReadWrite.RLock()
	defer globalReadWrite.Lock()
}

func use() {
	generic(&global)
	parameter(&global, &globalReadWrite)
	insideDeferred(&global)
	inLoop(1)
	inBranch(true, &global)
	otherBlock(true)
	throughInterface(&global)
	ownLock(&ownMutex{})
	(&guarded{pointer: &global}).field()
	(&guarded{pointer: &global}).pointerField()
	(&embedded{}).promoted()
}
