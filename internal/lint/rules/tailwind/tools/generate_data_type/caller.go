package main

import "runtime"

// runtimeCaller locates this source file so the sibling enumerate.mjs can be found regardless of
// the working directory the tool is invoked from.
func runtimeCaller() (uintptr, string, int, bool) {
	return runtime.Caller(0)
}
