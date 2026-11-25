//go:build go1.7

package parity

import "time"

// A file tagged older than the module, which is older than go1.21, has its tag's standard library, which has no
// time.Until, so neither reports here, in either branch. Its language version is go1.21, the oldest a tagged file
// is given, so the gate on the standard library's version is the one that holds these back.
func taggedOlder(t time.Time) {
	_ = t.Sub(time.Now())
	_ = (t.Sub)(time.Now())
}
