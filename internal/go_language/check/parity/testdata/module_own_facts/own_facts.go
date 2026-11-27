// Package own holds, for each fact kind cohere ports and compares, an unexported function that carries the fact and an
// exported twin that reaches it, so the own-facts differential reads the facts no importer inherits beside those it
// does.
package own

import (
	"fmt"
	"strings"
)

// double is pure.
func double(value int) int { return value * 2 }

// Double is pure through double.
func Double(value int) int { return double(value) }

// fresh never returns nil.
func fresh() *int {
	value := 1
	return &value
}

// Fresh never returns nil, through fresh.
func Fresh() *int { return fresh() }

// pairs needs its values of even length, which it checks itself.
func pairs(values []string) *strings.Replacer {
	if len(values)%2 != 0 {
		panic("pairs takes old and new strings in pairs")
	}
	return strings.NewReplacer(values...)
}

// Pairs needs its values of even length, through pairs.
func Pairs(values []string) *strings.Replacer { return pairs(values) }

// logf is a printf wrapper.
func logf(format string, arguments ...any) { fmt.Printf(format, arguments...) }

// Logf is a printf wrapper, through logf.
func Logf(format string, arguments ...any) { logf(format, arguments...) }
