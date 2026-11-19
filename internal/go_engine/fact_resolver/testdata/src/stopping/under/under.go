// Package under declares two functions that never return, one stopping calls and one it never does.
package under

// Abort never returns, and stopping calls it, so stopping's analysis imports its fact.
func Abort() {
	panic("abort")
}

// Unused never returns, and stopping never calls it, so stopping's analysis never reads its fact.
func Unused() {
	panic("unused")
}
