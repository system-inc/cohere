//go:build !(darwin || linux)

package main

import (
	"errors"
	"syscall"
)

// processNoun is what a process limit would count here, where a bounded run is refused.
const processNoun = "processes"

// errorUnbounded says why a bounded run is refused here: it counts processes and holds them to a limit as
// macOS and Linux do, the machines the house develops and tests on.
var errorUnbounded = errors.New("a bounded run runs on macOS and Linux, which count an account's processes")

func accountProcessCount() (int, error) {
	return 0, errorUnbounded
}

func holdProcessLimit(int) error {
	return errorUnbounded
}

// ownGroup is no group here, where begin refuses a bounded run before any command starts.
func ownGroup() *syscall.SysProcAttr {
	return nil
}

func signalGroup(int, syscall.Signal) { /* begin refuses a bounded run here, so there is no group. */ }

func groupAlive(int) bool {
	return false
}
