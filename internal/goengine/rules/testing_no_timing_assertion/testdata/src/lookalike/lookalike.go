package lookalike

import (
	"testing"
	"time"
)

// TestShaped has a test's name and signature in a file go test does not read for tests, so it is no test.
func TestShaped(test *testing.T) {
	start := time.Now()
	if time.Since(start) > time.Second {
		test.Fatal("too slow")
	}
}
