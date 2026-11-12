// Package cache_directory makes cohere's cache directory in a project, `<root>/.cache/cohere`, so that it ignores
// itself: the call that creates the directory writes a `.gitignore` in it holding `*`, which git reads as ignoring
// every file beside it and itself, so a run in a repository whose own rules say nothing of `.cache/` still leaves
// `git status` as it found it (#6vhp0vd). Go's build cache and pytest's do the same.
//
// Only the call that made the directory writes the file, and never over one that is there: a directory that already
// existed is the project's to keep as it is, a cache an older cohere made without the file among them, which the
// front door's note still names (prepareCacheDirectory). The front door, the TypeScript cache table and content
// pack, and the Go engine's store each make the directory through Make, and the Swift engine through its own
// CacheDirectory (swift/Sources/CohereSwift/Pipeline/CacheDirectory.swift), which writes the same bytes.
package cache_directory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// IgnoreFileContents is what the ignore file holds: a comment saying whose it is, then `*`, which matches the file
// itself too, so nothing under the directory shows as untracked.
const IgnoreFileContents = "# cohere's cache, made by cohere and ignored whole, this file with it.\n*\n"

// ignoreFileName is git's per-directory ignore file.
const ignoreFileName = ".gitignore"

// Of is root's cache directory, where cohere keeps what it caches for one project: `<root>/.cache/cohere`.
//
// It lived in the user cache under a hash of the root path, which nothing ever reclaimed. A moved or deleted project
// left its table behind for good, and 38 of them had piled up by 2026-10-02. In the project it is one folder per
// project that deleting is always a correct answer for, and a worktree has its own because it is its own root.
func Of(root string) string {
	return filepath.Join(root, ".cache", "cohere")
}

// Make makes directory, a project's cache directory, and every parent it needs. When this call is the one that
// created directory, it writes the ignore file in it; one that found it there writes nothing.
//
// Mkdir rather than MkdirAll for directory itself, since only Mkdir says whether this call created it: of two runs
// creating it at once, exactly one is told it did. The ignore file is created exclusively, so a file put there in
// between is never written over.
func Make(directory string) error {
	if createParentError := os.MkdirAll(filepath.Dir(directory), 0o755); createParentError != nil {
		return createParentError
	}
	createDirectoryError := os.Mkdir(directory, 0o755)
	if errors.Is(createDirectoryError, fs.ErrExist) {
		information, statError := os.Stat(directory)
		if statError != nil {
			return statError
		}
		if !information.IsDir() {
			return fmt.Errorf("%s is a file, not a directory", directory)
		}
		return nil
	}
	if createDirectoryError != nil {
		return createDirectoryError
	}
	return writeIgnoreFile(directory)
}

// writeIgnoreFile creates the ignore file in directory, never over one that is there.
func writeIgnoreFile(directory string) error {
	path := filepath.Join(directory, ignoreFileName)
	file, openError := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(openError, fs.ErrExist) {
		return nil
	}
	if openError != nil {
		return openError
	}
	_, writeError := file.WriteString(IgnoreFileContents)
	closeError := file.Close()
	if joinedError := errors.Join(writeError, closeError); joinedError != nil {
		// Ignored: the write already failed, and a file cut short could hold the comment without the `*`, which
		// ignores nothing while looking like it does.
		_ = os.Remove(path)
		return fmt.Errorf("writing %s: %w", path, joinedError)
	}
	return nil
}
