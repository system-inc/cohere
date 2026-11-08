package parity

import (
	. "time"
)

// A dot import names time.Since without its package.
func dotImported(start Time) {
	defer record(Since(start)) // want "call to time.Since is not deferred"
	defer record(Until(start))
}
