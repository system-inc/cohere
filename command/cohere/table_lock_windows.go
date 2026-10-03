package main

import "time"

// On Windows the dispatcher always waits for the engine (takeVerdictFile), so no run writes the table
// after its caller has moved on and there is nothing to keep in order.

func holdTableLock(tablePath string) func() {
	return func() {}
}

func waitForTableWriter(tablePath string, bound time.Duration) bool {
	return true
}
