package main

import (
	"fmt"
	"io/fs"
	"strings"
	"syscall"
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

// A process whose /proc status can't be read because it ended, as a missing file or as `ESRCH` caught mid-exit, or
// because it is another account's, is left out of the count; any other failure of the read fails the count.
func TestAnEndedOrForeignProcessIsLeftOutOfTheCount(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		failure error
		left    bool
	}{
		{&fs.PathError{Op: "open", Path: "/proc/1/status", Err: syscall.ENOENT}, true},
		{&fs.PathError{Op: "read", Path: "/proc/1/status", Err: syscall.ESRCH}, true},
		{&fs.PathError{Op: "open", Path: "/proc/1/status", Err: syscall.EACCES}, true},
		{&fs.PathError{Op: "read", Path: "/proc/1/status", Err: syscall.EIO}, false},
	} {
		if left := notThisAccounts(row.failure); left != row.left {
			test.Errorf("a read failing with %v is left out: %t, want %t", row.failure, left, row.left)
		}
	}
}
