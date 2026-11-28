package main

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// About 50 packages reran on every Shackler integration whatever the change, after the tree's files, directories and
// .git were all stamped (J-bacpn4p 59, J-z8xxkjp 58, J-khz1g7j 52 for one test file changed, #ntrewm3), and the test
// environment was the same in all three. What is left for Go's test cache to see moving is what a test reads inside the
// module that the stamp doesn't hold: a path the tree doesn't track, a build's output or a test's scratch, which a
// listing of its directory names; or a stamped path whose time moved since, written by something in the job.
// treeOffTheStamp names both, so the account says them before the tests and after, from the box's own run, rather
// than the cause being inferred from a probe on another machine.

// treeOffTheStamp walks tree, a laid worktree stampTreeTimes stamped, and names every path off the stamp: `untracked
// <path>` for an entry `HEAD` doesn't track, its directory once with nothing beneath it named, and `moved <path>
// <time>` for a tracked file, directory, the root or .git whose time isn't the stamp's. A submodule's gitlink is
// passed over, since it is a tree of its own, walked by its own call, and so is a symbolic link, which the stamp leaves
// as it is. A tracked file that differed from `HEAD` at the stamp, which it left its own time, reads as moved, as it
// is to Go.
func treeOffTheStamp(tree string) ([]string, error) {
	listing, listError := gitOutput(tree, "ls-tree", "-r", "-t", "-z", "--full-tree", "HEAD")
	if listError != nil {
		return nil, fmt.Errorf("listing the tree's HEAD: %w", listError)
	}
	// What each tracked path's time should be, and the paths the walk passes over.
	stamps := map[string]time.Time{".": directoryTime, ".git": directoryTime}
	passed := map[string]bool{}
	for _, line := range strings.Split(listing, "\x00") {
		if line == "" {
			continue
		}
		header, path, found := strings.Cut(line, "\t")
		fields := strings.Fields(header)
		if !found || len(fields) != 3 {
			return nil, fmt.Errorf("git ls-tree said %q, not a mode, a type, an id and a path", line)
		}
		mode, kind, id := fields[0], fields[1], fields[2]
		switch {
		case kind == "commit" || mode == "120000":
			passed[path] = true
		case kind == "tree":
			stamps[path] = directoryTime
		default:
			stamp, timeError := objectTime(id)
			if timeError != nil {
				return nil, fmt.Errorf("reading %s's stamp: %w", path, timeError)
			}
			stamps[path] = stamp
		}
	}
	off := []string{}
	walkError := filepath.WalkDir(tree, func(place string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		relative, relativeError := filepath.Rel(tree, place)
		if relativeError != nil {
			return relativeError
		}
		path := filepath.ToSlash(relative)
		if passed[path] {
			return skipped(entry)
		}
		stamp, tracked := stamps[path]
		if !tracked {
			if entry.IsDir() {
				off = append(off, "untracked "+path+"/")
				return fs.SkipDir
			}
			off = append(off, "untracked "+path)
			return nil
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		if !information.ModTime().Equal(stamp) {
			off = append(off, "moved "+path+" "+information.ModTime().UTC().Format(time.RFC3339Nano))
		}
		// Inside .git is git's own, which Go reads only through the .git entry's own stat.
		if path == ".git" {
			return skipped(entry)
		}
		return nil
	})
	if walkError != nil {
		return nil, fmt.Errorf("walking %s: %w", tree, walkError)
	}
	return off, nil
}

// skipped is what the walk returns to pass over entry: its whole tree when it is a directory, else nothing more.
func skipped(entry fs.DirEntry) error {
	if entry.IsDir() {
		return fs.SkipDir
	}
	return nil
}
