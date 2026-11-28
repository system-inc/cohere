package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// The integrate job's tests write nothing into the tree it laid (#d49qcjg). A test that did moved a directory's listing
// or a file's time, so its key was saved with an input the next job's fresh tree doesn't have, and about 50 packages
// reran on every landing whatever it changed (J-dqg0kkx). Two ways keep it out, each for what the other can't see:
//
//   - holdTreeStill makes every directory of the laid tree read-only for the tests, so whatever creates, renames or
//     removes an entry is refused by the kernel, in its own test, naming its own path, and the landing is refused
//     with that test named.
//   - The walk after the tests (treeOffTheStamp) refuses a landing whose tree is off its stamp, which is how a tracked
//     file rewritten in place shows: a read-only directory still lets a writable file be written.

// heldDirectory is one directory holdTreeStill made read-only and the mode it had, which its release puts back.
type heldDirectory struct {
	path string
	mode fs.FileMode
}

// holdTreeStill makes every directory under tree read-only, tree itself and a submodule's among them, but for what is
// inside a .git directory, git's own, which the tests' git may write. It returns the release that gives each directory
// back its own mode, which the caller runs on every way out: the checks after the tests write cohere's cache into the
// tree, and the next job's lay replaces it. A directory it can't make read-only is released at once and refused, so a
// hold never stands half made.
func holdTreeStill(tree string) (func() error, error) {
	held := []heldDirectory{}
	release := func() error {
		var releaseErrors []error
		// Deepest last held, first given back, though a mode never depends on its parent's.
		for index := len(held) - 1; index >= 0; index-- {
			if changeError := os.Chmod(held[index].path, held[index].mode); changeError != nil {
				releaseErrors = append(releaseErrors, changeError)
			}
		}
		held = nil
		return errors.Join(releaseErrors...)
	}
	walkError := filepath.WalkDir(tree, func(place string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		if !entry.IsDir() {
			return nil
		}
		if entry.Name() == ".git" && place != tree {
			return fs.SkipDir
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		mode := information.Mode().Perm()
		if changeError := os.Chmod(place, mode&^0o222); changeError != nil {
			return changeError
		}
		held = append(held, heldDirectory{path: place, mode: mode})
		return nil
	})
	if walkError != nil {
		if releaseError := release(); releaseError != nil {
			return nil, fmt.Errorf("holding %s still: %w, and giving back what was held: %w", tree, walkError,
				releaseError)
		}
		return nil, fmt.Errorf("holding %s still: %w", tree, walkError)
	}
	return release, nil
}
