//go:build go1.23

package versions

import (
	"iter"
	"log/slog"
	"maps"
	"slices"
	"strings"
)

// lines uses strings.Lines, from go1.24, newer than this file's go1.23.
func lines(text string) iter.Seq[string] {
	return strings.Lines(text)
}

// fields uses strings.SplitSeq, from go1.24.
func fields(text string) []string {
	return slices.Collect(strings.SplitSeq(text, " "))
}

// discarding uses slog.DiscardHandler, from go1.24, a variable.
func discarding() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// counted calls lines and fields, and uses slices.Collect and maps.Keys, from go1.23, which this file's version
// holds, so it reports nothing itself.
func counted(text string, seen map[string]bool) int {
	return len(slices.Collect(lines(text))) + len(fields(text)) + len(slices.Collect(maps.Keys(seen)))
}
