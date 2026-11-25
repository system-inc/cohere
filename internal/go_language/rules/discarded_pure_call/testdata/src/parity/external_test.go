package parity_test

import (
	"strings"
	"testing"
)

// Found: an external test file is a test file too, and a test there takes no *testing.B.
func TestExternal(test *testing.T) {
	strings.TrimSpace(" text ")
}

// Not found: a benchmark of the external test package.
func BenchmarkExternal(benchmark *testing.B) {
	strings.TrimSpace(" text ")
}
