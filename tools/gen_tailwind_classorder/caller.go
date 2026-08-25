package main

import "runtime"

// thisFile reports this file's own path, so the sibling scripts are found next to it rather than
// relative to whatever working directory the command was invoked from.
func thisFile() string {
	_, path, _, _ := runtime.Caller(0)
	return path
}
