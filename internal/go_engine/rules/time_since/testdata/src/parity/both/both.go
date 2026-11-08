package both

import (
	"time"
	. "time"
	clock "time"
)

// Where the file imports time as time beside a second name and its own scope, time names the import wherever the
// call stands, so the original's time.Since is right, and the port writes it too, to the byte.
func both(t Time) Duration {
	_ = clock.Now().Sub(t)
	_ = (clock.Now)().Sub(t)
	_ = Now().Sub(t)
	return time.Now().Sub(t)
}
