// Package differences sits under a stand-in for the user's cache directory, beside a staticcheck.conf that allows its
// dot import, so the original and the port differ on it (#sc5qmy7).
package differences

import (
	// staticcheck's config analyzer skips every file under the user's cache directory as one of Go's build cache, so
	// the original reads no staticcheck.conf here and reports the import; the port reads it through the engine's own,
	// which skips only the build caches go env names, and leaves alone the path it allows (#sc5qmy7).
	. "strings" // port drops: should not use dot imports
)

var _ = ToUpper
