// Package halt is a dependency of parity whose functions never return, or return only sometimes, so the parity
// fixture reaches ctrlflow's noReturn facts from another package.
package halt

import (
	"os"
	"runtime"
)

// Exit never returns: it calls os.Exit.
func Exit() {
	os.Exit(2)
}

// ExitThrough never returns: it calls Exit, a function of its own package that never returns.
func ExitThrough() {
	Exit()
}

// Stop never returns: runtime.Goexit is one of ctrlflow's intrinsics.
func Stop() {
	runtime.Goexit()
}

// Fail never returns: it panics.
func Fail(reason string) {
	panic(reason)
}

// Maybe returns when stop is false.
func Maybe(stop bool) {
	if stop {
		os.Exit(3)
	}
}

// Fatal is a type whose method never returns.
type Fatal struct{}

// Die never returns.
func (Fatal) Die() {
	panic("die")
}

// Forever never returns: it loops without end.
func Forever() {
	for {
		runtime.Gosched()
	}
}

// Generic never returns, whatever its type argument.
func Generic[T any](value T) {
	panic(value)
}
