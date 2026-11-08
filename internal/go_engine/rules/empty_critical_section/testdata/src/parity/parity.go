// Package parity holds pairs of calls that lock and at once unlock, and pairs that do not, every shape the check reads
// a call or a receiver in, where the port and staticcheck's SA2001 still agree finding for finding. The differential
// test runs both over each and compares.
package parity

import (
	"sync"

	"locker"
)

type custom struct{}

func (custom) Lock()   {}
func (custom) Unlock() {}

type withArgument struct{}

func (withArgument) Lock(times int) {}
func (withArgument) Unlock()        {}

type withField struct {
	lock   func()
	unlock func()
}

type lockable interface {
	Lock()
	Unlock()
}

func work() {}

// Found by both: a mutex.
func mutexValue() {
	var mutex sync.Mutex
	mutex.Lock()
	mutex.Unlock()
}

// Found by both: a reader lock.
func readerLock() {
	var mutex sync.RWMutex
	mutex.RLock()
	mutex.RUnlock()
}

// Found by both: the call statements in parentheses, which are looked through.
func parenthesizedCalls() {
	var mutex sync.Mutex
	(mutex.Lock())
	(mutex.Unlock())
}

// Found by both: a receiver through a pointer.
func dereferenced(pointer *sync.Mutex) {
	(*pointer).Lock()
	(*pointer).Unlock()
}

// Found by both: a receiver indexed out of a slice.
func indexed(mutexes []sync.Mutex) {
	mutexes[0].Lock()
	mutexes[0].Unlock()
}

// Found by both: a type of its own with Lock and Unlock.
func customType() {
	var value custom
	value.Lock()
	value.Unlock()
}

// Found by both: a package's functions, called through its name.
func packageFunctions() {
	locker.Lock()
	locker.Unlock()
}

// Found by both: an interface's methods.
func interfaceMethods(value lockable) {
	value.Lock()
	value.Unlock()
}

// Found by both: a type parameter's methods.
func typeParameter[T lockable](value T) {
	value.Lock()
	value.Unlock()
}

// Found by both: inside a function literal's body.
func insideLiteral(mutex *sync.Mutex) func() {
	return func() {
		mutex.Lock()
		mutex.Unlock()
	}
}

// Found by both, twice: two empty sections in a row, and the unlock then lock between them is no section.
func twoInARow(mutex *sync.Mutex) {
	mutex.Lock()
	mutex.Unlock()
	mutex.Lock()
	mutex.Unlock()
}

// Found by both: inside an if's body.
func insideIf(mutex *sync.Mutex, condition bool) {
	if condition {
		mutex.Lock()
		mutex.Unlock()
	}
}

// Found by neither: work between the two.
func workBetween(mutex *sync.Mutex) {
	mutex.Lock()
	work()
	mutex.Unlock()
}

// Found by neither: the unlock deferred.
func deferred(mutex *sync.Mutex) {
	mutex.Lock()
	defer mutex.Unlock()
	work()
}

// Found by neither: unlock then lock.
func unlockThenLock(mutex *sync.Mutex) {
	mutex.Unlock()
	mutex.Lock()
}

// Found by neither: a lock and a reader unlock.
func mismatched(mutex *sync.RWMutex) {
	mutex.Lock()
	mutex.RUnlock()
}

// Found by neither: two receivers.
func twoReceivers(first *sync.Mutex, second *sync.Mutex) {
	first.Lock()
	second.Unlock()
}

// Found by neither: the same receiver printed two ways.
func printedTwoWays(mutex *sync.Mutex) {
	(mutex).Lock()
	mutex.Unlock()
}

// Found by neither: the method expression in parentheses, which are not looked through.
func parenthesizedMethod(mutex *sync.Mutex) {
	(mutex.Lock)()
	(mutex.Unlock)()
}

// Found by neither: a Lock that takes an argument.
func lockWithArgument() {
	var value withArgument
	value.Lock(1)
	value.Unlock()
}

// Found by neither: fields of function type, not methods.
func functionFields(value withField) {
	value.lock()
	value.unlock()
}

// Found by neither: in a switch's case, whose statements are no block.
func inCase(mutex *sync.Mutex, choice int) {
	switch choice {
	case 1:
		mutex.Lock()
		mutex.Unlock()
	}
}

// Found by neither: in a select's case, whose statements are no block either.
func inSelect(mutex *sync.Mutex, channel chan int) {
	select {
	case <-channel:
		mutex.Lock()
		mutex.Unlock()
	}
}

// Found by neither: the unlock in a goroutine.
func inGoroutine(mutex *sync.Mutex) {
	mutex.Lock()
	go mutex.Unlock()
}

// Found by neither: a block of one statement.
func single(mutex *sync.Mutex) {
	mutex.Lock()
}
