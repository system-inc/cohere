package concurrency_tests

import (
	"slices"
	"strings"
	"testing"
)

// The -race list is read from planted sources as race_gated.go holds it, and every shape the guard can't read is
// refused in words, never read as a shorter list (the Go Square's S2 and S3 on #0zdyk51).
func TestTheRaceListIsReadOrRefused(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		name, source string
		want         []string
		refused      string
	}{
		{"a list", `var raceGated = []raceEntry{{directory: "a", reason: "r"}, {reason: "s", directory: "b/c"}}`,
			[]string{"a", "b/c"}, ""},
		{"no list", `var other = []raceEntry{{directory: "a"}}`, nil, "declares no raceGated"},
		{"a second name", `var raceGated, other = []raceEntry{{directory: "a"}}, 1`, nil, "beside another name"},
		{"declared twice", "var raceGated = []raceEntry{{directory: \"a\"}}\nvar (raceGated = []raceEntry{})", nil,
			"declares raceGated twice"},
		{"no literal", `var raceGated = entries()`, nil, "is no list literal"},
		{"an entry no literal", `var raceGated = []raceEntry{first}`, nil, "an entry that is no literal"},
		{"a directory no literal", `var raceGated = []raceEntry{{directory: root + "/a"}}`, nil,
			"no directory written as a string literal"},
		{"an entry with no directory", `var raceGated = []raceEntry{{reason: "r"}}`, nil,
			"no directory written as a string literal"},
		{"an empty list", `var raceGated = []raceEntry{}`, nil, "names no directory"},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			got, readError := RaceGatedDirectories("race_gated.go", []byte("package main\n\n"+row.source+"\n"))
			if row.refused == "" {
				if readError != nil || !slices.Equal(got, row.want) {
					test.Errorf("read %q (%v), want %q", got, readError, row.want)
				}
				return
			}
			if readError == nil || !strings.Contains(readError.Error(), row.refused) {
				test.Errorf("read %q (%v), want it refused saying %q", got, readError, row.refused)
			}
		})
	}
}
