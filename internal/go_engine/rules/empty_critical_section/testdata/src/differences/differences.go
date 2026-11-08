// Package differences holds the pairs where the port and the original could plausibly disagree, and records no
// difference, since there is none: both judge receivers by staticcheck's own printer. Receivers that print alike but
// are two values, and receivers holding a comment the printer leaves out, are judged by what Render prints.
package differences

import "sync"

type holder[T any] struct {
	mutex sync.Mutex
	value T
}

// Found by both: two fresh mutexes that print alike, a false positive the original makes.
func freshMutexes() {
	(&sync.Mutex{}).Lock()
	(&sync.Mutex{}).Unlock()
}

// Found by both: a receiver through a generic type's instantiation.
func generic(value *holder[int]) {
	value.mutex.Lock()
	value.mutex.Unlock()
}

// Found by both: a comment inside one receiver, which Render does not print.
func commentInside(mutexes map[string]*sync.Mutex) {
	mutexes[ /* the first */ "key"].Lock()
	mutexes["key"].Unlock()
}

// Found by both: an embedded mutex's promoted methods.
func promoted(value *struct{ sync.Mutex }) {
	value.Lock()
	value.Unlock()
}
