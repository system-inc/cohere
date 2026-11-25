// Package chain holds a function that ends the process on windows alone, through a function each platform's file
// declares, a caller of it that returns in every build, a caller of that caller, and a caller of a function the
// standard library declares in a shared file, which calls one each platform's file declares, each in this shared file.
package chain

import "os"

// Finish ends the process on windows and returns everywhere else.
func Finish() {
	platformFinish()
}

// Maybe calls Finish only when asked to, so it returns in every build.
func Maybe(stop bool) {
	if stop {
		Finish()
	}
}

// Twice calls Maybe, so it reaches Finish through a caller whose own fact is the same in every build.
func Twice() {
	Maybe(false)
	Maybe(true)
}

// Process is the process's ID, which os reads from syscall, whose function each platform's file declares.
func Process() int {
	return os.Getpid()
}
