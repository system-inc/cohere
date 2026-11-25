package load

import (
	"slices"
	"testing"
)

// The tags of `GOFLAGS` are its last `-tags=`, in either dash form, split at commas; other flags set none.
func TestTagsOfReadsTheLastTagsFlag(test *testing.T) {
	test.Parallel()

	for goFlags, want := range map[string][]string{
		"":                              nil,
		"-buildvcs=false":               nil,
		"-tags=special":                 {"special"},
		"-buildvcs=false -tags=one,two": {"one", "two"},
		"--tags=first -tags=second":     {"second"},
		"-tags=first -tags=":            nil,
	} {
		test.Run(goFlags, func(test *testing.T) {
			test.Parallel()

			if got := tagsOf(goFlags); !slices.Equal(got, want) {
				test.Fatalf("want %q, got %q", want, got)
			}
		})
	}
}
