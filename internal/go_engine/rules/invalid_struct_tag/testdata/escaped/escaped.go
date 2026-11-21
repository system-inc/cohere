// Package escaped imports go-flags spelled with an escape, which names the same path, so the keys go-flags repeats are
// left alone in it, and every other key repeated is still found.
package escaped

import _ "github.com/jessevdk/go\x2dflags"

type options struct {
	// The original compares the import's raw text, reads no go-flags import here, and reports the key go-flags repeats
	// on purpose.
	A []int `default:"foo" default:"bar"` // port drops: duplicate struct tag "default"
	B int   `long:"foo" long:"bar"`
}
