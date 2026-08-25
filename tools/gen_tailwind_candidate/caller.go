package main

import "runtime"

// runtimeCaller reports this file's own path, so enumerate.mjs is found next to it rather than
// relative to the working directory the command was invoked from.
func runtimeCaller() (uintptr, string, int, bool) {
	return runtime.Caller(0)
}
