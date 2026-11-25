package parity

import (
	"strings"
	"testing"
)

// Found, shown at elsewhere_test.go: the file was parsed as directive.go, so its function taking a *testing.B is no
// benchmark's, whatever name the directive below gives its lines.
//
//line elsewhere_test.go:1
func underTestDirective(benchmark *testing.B) {
	strings.ToTitle("text")
	_ = benchmark
}
