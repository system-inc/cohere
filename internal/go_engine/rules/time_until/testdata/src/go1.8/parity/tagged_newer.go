//go:build go1.9

package parity

import "time"

// A file tagged newer than the module has its tag's standard library, which has time.Until.
func taggedNewer(t time.Time) {
	_ = t.Sub(time.Now())
}
