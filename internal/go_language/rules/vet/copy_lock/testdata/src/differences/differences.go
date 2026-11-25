// Package differences holds code that does not type-check, where the original panics and the port is right not to.
// An assignment with more values than operands, `=` or `:=`, is reported by the type checker as an assignment
// mismatch, and the type checker still types every value. The original, which runs despite type errors, then reads
// the operand at a lock's index, past the end of the operands, and panics with `index out of range [1] with length
// 1` at copylock.go:90, which stops every analyzer in the run. The port reports nothing for a value with no operand,
// and still reports a value that has one. The original cannot run here, so the test runs the port alone.
package differences

import "sync"

func assignment() {
	var mutex sync.Mutex
	var first int
	first = 1, mutex
	_ = first
}

func loop() {
	var mutex sync.Mutex
	for index := 0, mutex; index < 1; index++ {
	}
}

func named() {
	var mutex sync.Mutex
	var copied sync.Mutex
	copied = mutex, 1 // want "assignment copies lock value to copied: sync.Mutex"
	_ = &copied
}
