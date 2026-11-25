package parity

import (
	. "sync"
	. "unsafe"
)

// A dot import names sync's types and unsafe's builtins without their package.

var dottedMutex Mutex

var dottedCopy = dottedMutex

func dotted(mutex Mutex, group *WaitGroup) {
	copied := mutex
	_ = Sizeof(mutex)
	_ = Alignof(mutex)
	_ = copied
}
