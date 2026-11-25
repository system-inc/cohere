package dotted

import (
	"time"
	. "time"
)

// The file imports time into its own scope beside its own name, so the fix's time names it, as the original's does.
func dotted(t Time) Duration {
	_ = (t.Sub)(Now())
	return t.Sub(Now())
}

var _ time.Duration
