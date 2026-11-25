package parity

import (
	"net/http"
	"testing"
)

// A test file's calls are judged like any other.
func TestStatus(test *testing.T) {
	_ = http.StatusText(502)
}
