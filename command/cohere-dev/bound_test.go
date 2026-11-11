package main

import (
	"fmt"
	"strings"
	"testing"
)

// The bounded flags are read wherever go's flags are, and only before exec's command, whose flags are its own;
// a margin or a deadline bounds a run on its own, and a value that bounds nothing is refused.
func TestBoundedFlagsAreReadBeforeTheCommandsOwn(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		arguments []string
		leading   bool
		kept      string
		bounded   string
	}{
		{[]string{"-run", "X", "./internal/edit"}, false, "-run X ./internal/edit", "none"},
		{[]string{"-run", "X", "--bounded", "./internal/edit"}, false, "-run X ./internal/edit", "512 15m0s"},
		{[]string{"--bounded-margin", "64", "./internal/edit"}, false, "./internal/edit", "64 15m0s"},
		{[]string{"--bounded-deadline=30s", "--bounded-margin=8", "."}, false, ".", "8 30s"},
		{[]string{"./internal/edit", "-args", "--bounded"}, false, "./internal/edit -args --bounded", "none"},
		{[]string{"--bounded", "--", "probe", "--bounded"}, true, "-- probe --bounded", "512 15m0s"},
		{[]string{"--bounded", "probe", "--bounded-margin", "1"}, true, "probe --bounded-margin 1", "512 15m0s"},
	} {
		kept, limit, readError := withoutBoundedFlags(row.arguments, row.leading)
		if readError != nil {
			test.Errorf("%q: %v", row.arguments, readError)
			continue
		}
		bounded := "none"
		if limit != nil {
			bounded = fmt.Sprintf("%d %s", limit.margin, limit.deadline)
		}
		if strings.Join(kept, " ") != row.kept || bounded != row.bounded {
			test.Errorf("%q read as %q bounded %s, want %q bounded %s",
				row.arguments, kept, bounded, row.kept, row.bounded)
		}
	}
	for _, arguments := range [][]string{
		{"--bounded=5"},
		{"--bounded-margin", "0"},
		{"--bounded-margin=many"},
		{"--bounded-deadline", "-1s"},
		{"--bounded-deadline=soon"},
		{"--bounded-deadline"},
	} {
		if _, _, readError := withoutBoundedFlags(arguments, false); readError == nil {
			test.Errorf("%q was not refused", arguments)
		}
	}
}
