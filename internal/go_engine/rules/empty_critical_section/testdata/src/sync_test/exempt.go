// Package sync_test is named as the sync package's external tests, which staticcheck exempts, so neither side reports
// the empty critical section it holds.
package sync_test

import "sync"

func exempt() {
	var mutex sync.Mutex
	mutex.Lock()
	mutex.Unlock()
}
