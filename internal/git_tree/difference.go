package git_tree

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// Difference is how a working-tree file isn't what its listing entry records. Each consumer words it
// in its own messages.
type Difference string

const (
	// Unchanged is a file that is what its entry records.
	Unchanged Difference = "Unchanged"
	// Missing is a path with nothing at it.
	Missing Difference = "Missing"
	// AnotherKind is a path holding another kind of thing than its entry's mode: a file where a link
	// is recorded, a link where a file is, a directory, a device.
	AnotherKind Difference = "AnotherKind"
	// AnotherExecutableBit is a file whose owner's executable bit disagrees with its entry's mode,
	// 100755 or 100644.
	AnotherExecutableBit Difference = "AnotherExecutableBit"
	// AnotherContent is a file or link of the recorded kind and bit whose blob isn't the entry's.
	AnotherContent Difference = "AnotherContent"
)

// Differs says how the file at name isn't what entry records, as `git diff-files` would find it:
// Missing, AnotherKind, AnotherExecutableBit (by the owner's bit alone, as ModeOf reads it), or
// AnotherContent, checked in that order and the first that holds returned, else Unchanged. The blob is
// hashed in the format the entry's object is written in. A gitlink is refused: whether a submodule is
// at its pin is a question for its own repository, not a file to hash.
//
// A path whose parent is no longer a directory is Missing, as git has it (check_removed in git's
// diff-lib.c takes `ENOTDIR` as removal). A file that changes size while it is hashed is an error.
func Differs(name string, entry Entry) (Difference, error) {
	if entry.Mode == ModeGitlink || entry.Mode == ModeTree {
		return "", fmt.Errorf("%s is recorded as mode %s, which is no file to compare", entry.Path, entry.Mode)
	}
	format, formatError := FormatOf(entry.Object)
	if formatError != nil {
		return "", fmt.Errorf("%s: %w", entry.Path, formatError)
	}
	information, lstatError := os.Lstat(name)
	if errors.Is(lstatError, fs.ErrNotExist) || errors.Is(lstatError, syscall.ENOTDIR) {
		return Missing, nil
	}
	if lstatError != nil {
		return "", lstatError
	}
	mode, modeError := ModeOf(information)
	// ModeOf refuses only a directory and what git doesn't track, each another kind than any entry
	// Differs compares.
	if modeError != nil {
		return AnotherKind, nil
	}
	if (mode == ModeSymbolicLink) != (entry.Mode == ModeSymbolicLink) {
		return AnotherKind, nil
	}
	if mode != entry.Mode {
		return AnotherExecutableBit, nil
	}
	object, hashError := hashBlobAt(format, name, mode, information)
	if hashError != nil {
		return "", hashError
	}
	if object != entry.Object {
		return AnotherContent, nil
	}
	return Unchanged, nil
}
