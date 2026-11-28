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
//
// Go's test cache keys a directory a test reads on its mode with the rest of its stat, so a key saved under the hold
// holds `r-x` for every directory of the tree. That is consistent because every integrate job's tests run held, and
// it is why they must: tests run on Shackler unheld, with the same tree and the same cache, replay nothing that reads a
// directory (the Go Square's note on #d49qcjg). The first job under the hold reruns those packages once, for the same
// reason.
//
// A job killed before it releases its hold, by a ceiling, a `SIGKILL` or the box going down, leaves its tree read-only,
// and the next job's lay checks out over it in place, which can't create or remove an entry there. So the lay gives
// the tree back first (giveTreeBack), whatever a job before it held.

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

// giveTreeBack gives every directory under tree that holdTreeStill made read-only its owner's write again, but for
// what is inside a .git directory, which the hold never touched: what a job killed while holding it left behind, which
// the lay must be able to check out over and clean. A directory its owner can already write is left as it is, and a
// tree that isn't there is nothing to give back.
func giveTreeBack(tree string) error {
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
		if mode := information.Mode().Perm(); mode&0o200 == 0 {
			return os.Chmod(place, mode|0o200)
		}
		return nil
	})
	if walkError != nil && !errors.Is(walkError, fs.ErrNotExist) {
		return fmt.Errorf("giving %s back its owner's write: %w", tree, walkError)
	}
	return nil
}
