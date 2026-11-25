package parity

import (
	"time"
	clock "time"
)

// The file imports time under its own name beside another, so the fix's time names it, as the original's does.
func renamed(t clock.Time) clock.Duration {
	return t.Sub(clock.Now())
}

var _ time.Duration
