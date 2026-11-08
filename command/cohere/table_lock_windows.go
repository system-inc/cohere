package main

import "time"

// On Windows the dispatcher always waits for the engine (takeVerdictFile), so no run writes the table
// after its caller has moved on and there is nothing to keep in order.

func holdTableLock(directory string) func() {
	return func() { /* No lock was taken, so there is nothing to release. */ }
}

func holdTableReadLock(directory string, bound time.Duration) (func(), bool) {
	return func() { /* No lock was taken, so there is nothing to release. */ }, true
}
