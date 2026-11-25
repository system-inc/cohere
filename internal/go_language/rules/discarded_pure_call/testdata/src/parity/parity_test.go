package parity

import (
	"strings"
	"testing"
)

// Found: a test without a *testing.B.
func TestDiscarded(test *testing.T) {
	strings.ToLower("text")
}

// Not found: a benchmark, and any function of a test file taking a *testing.B, wherever it is in the list.
func BenchmarkDiscarded(benchmark *testing.B) {
	strings.ToLower("text")
}

func helper(text string, benchmark *testing.B) {
	strings.ToLower(text)
	_ = benchmark
}

func (receiver record) benchmarkMethod(benchmark *testing.B) {
	strings.ToLower("text")
	_ = benchmark
}

// Found: a testing.B taken by value is no *testing.B.
func helperByValue(text string, benchmark testing.B) {
	strings.ToLower(text)
	_ = benchmark
}

// Found: a closure inside a benchmark is a function of its own, which takes no *testing.B.
func BenchmarkClosure(benchmark *testing.B) {
	func() {
		strings.ToLower("text")
	}()
}
