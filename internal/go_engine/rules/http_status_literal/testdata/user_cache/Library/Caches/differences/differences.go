// Package differences sits under a stand-in for the user's cache directory, beside a staticcheck.conf that leaves 418
// alone, so the original and the port differ on it (#sc5qmy7).
package differences

import "net/http"

func statuses() {
	// staticcheck's config analyzer skips every file under the user's cache directory as one of Go's build cache, so
	// the original reads no staticcheck.conf here and reports the code; the port reads it through the engine's own,
	// which skips only the build caches go env names, and leaves alone the code it allows (#sc5qmy7).
	_ = http.StatusText(418) // port drops: should use constant http.StatusTeapot instead of numeric literal 418
}
