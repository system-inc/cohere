// Package differential names where a formatter's output first departs from the output it was held to.
//
// It once measured candidate formatters against the Prettier fork over whole corpora; the fork has left cohere
// (#mjecaaw), and what its comparisons shared, naming the first difference, is what the golden-backed tests
// still use.
package differential

import (
	"fmt"
	"strings"
)

// FirstDifference names the first line where two outputs diverge.
//
// A line, not a byte offset, because the reader is going to open the file. Both lines are quoted so a
// whitespace-only difference is visible rather than looking like two identical strings.
func FirstDifference(expected string, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for line := 0; line < len(expectedLines) || line < len(actualLines); line++ {
		var want string
		var got string
		if line < len(expectedLines) {
			want = expectedLines[line]
		}
		if line < len(actualLines) {
			got = actualLines[line]
		}
		if want != got || line >= len(expectedLines) || line >= len(actualLines) {
			return fmt.Sprintf("line %d: want %q, got %q", line+1, truncate(want), truncate(got))
		}
	}
	return "identical by line, different by bytes (a trailing newline or a carriage return)"
}

func truncate(line string) string {
	const limit = 120
	if len(line) <= limit {
		return line
	}
	return line[:limit] + "…"
}
