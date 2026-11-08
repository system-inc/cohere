package parity

import (
	"strings"
	"testing"
)

// Not found, though shown at elsewhere.go: the file was parsed as directive_test.go, so its function taking a
// *testing.B is spared, whatever name the directive below gives its lines.
//
//line elsewhere.go:1
func underPlainDirective(benchmark *testing.B) {
	strings.ToTitle("text")
	_ = benchmark
}
