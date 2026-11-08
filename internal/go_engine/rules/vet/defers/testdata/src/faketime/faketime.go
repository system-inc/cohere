// Package time is a package named time at a path that is not the standard library's, so a function named Since in it
// is not time.Since.
package time

import realtime "time"

// Since has time.Since's name and signature, at another path.
func Since(start realtime.Time) realtime.Duration {
	return 0
}
