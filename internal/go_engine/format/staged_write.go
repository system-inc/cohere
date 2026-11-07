package format

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
)

// WriteAllAtomically replaces each file of files with its contents as one set, for files that only make sense
// together, as a rename's do: a file renamed alone leaves its references broken. Every file's new contents are
// staged in a temporary file beside it first, and only once every one is staged are they renamed into place, in the
// order of their names. A file that cannot be read, staged or written removes every temporary file and writes
// nothing. A rename into place that fails once others are in place, which a temporary file beside its target leaves
// unlikely, puts the files already replaced back as they were, and partlyWritten names any it could not put back, the
// only case that leaves the set half written.
func WriteAllAtomically(files map[string][]byte) (partlyWritten []string, writeError error) {
	return writeAllAtomically(files, putInPlace)
}

// writeAllAtomically is WriteAllAtomically with the step that renames a staged file into place given, so a test can
// fail a placement after others have succeeded, the one failure a real file system leaves too rare to stage, and see
// the files already placed put back.
func writeAllAtomically(
	files map[string][]byte,
	place func(stagedFile) error,
) (partlyWritten []string, writeError error) {
	names := slices.Sorted(maps.Keys(files))
	originals := make(map[string][]byte, len(names))
	staged := make([]stagedFile, 0, len(names))
	removeStaged := func(from int) {
		for _, each := range staged[from:] {
			// Ignored: the temporary file is ours, and the error worth returning is the one that stopped the set.
			_ = os.Remove(each.temporary)
		}
	}
	for _, file := range names {
		original, readError := os.ReadFile(file)
		if readError != nil {
			removeStaged(0)
			return nil, fmt.Errorf("reading %s before writing the set: %w", file, readError)
		}
		originals[file] = original
		each, stageError := stage(file, files[file])
		if stageError != nil {
			removeStaged(0)
			return nil, stageError
		}
		staged = append(staged, each)
	}
	for index, each := range staged {
		renameError := place(each)
		if renameError == nil {
			continue
		}
		removeStaged(index + 1)
		for _, placed := range names[:index] {
			if restoreError := WriteAtomically(placed, originals[placed]); restoreError != nil {
				partlyWritten = append(partlyWritten, placed)
				renameError = errors.Join(renameError, restoreError)
			}
		}
		return partlyWritten, renameError
	}
	return nil, nil
}
