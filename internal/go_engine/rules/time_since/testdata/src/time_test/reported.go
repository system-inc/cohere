package time_test

import "time"

// The time package's external tests are a package of their own, which imports time, so the type index holds its
// symbols and both report here: only the package whose path is time is skipped.
func elapsed(t time.Time) time.Duration {
	return time.Now().Sub(t)
}
