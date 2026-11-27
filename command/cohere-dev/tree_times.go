package main

import (
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A tree laid fresh in each integrate job reads to Go's test cache as all new (#e94a6r4, J-wk7r0nz): a test's result
// replays only when every file it opened or statted inside the module has the size, mode and modification time it had
// when the result was saved (cmd/go's computeTestInputsID), and a checkout stamps every file with the moment it was
// written. So the first warm run compiled from the cache and replayed 70 of 438 packages' tests.
//
// stampTreeTimes gives each tracked file and directory of a laid tree a modification time drawn from its git object id
// (objectTime), so the same content laid again, in any job, reads the same, and changed content reads changed: its
// blob or tree id is new, so is its time, and its tests run. Go never reads a file's bytes for this key, so the time is
// what stands in for them, and it now follows them. A file the tree doesn't track, a build's output or a test's
// scratch, keeps the time it was written, and reads as new, which it is.

// objectTimeEpoch is where objectTime's range starts, and objectTimeSpan its width in seconds: 1980 to 1997, so a
// stamped time is always years in the past and never meets Go's rule that a file younger than two seconds can't be
// cached (cmd/go's modTimeCutoff).
var (
	objectTimeEpoch = time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	objectTimeSpan  = uint64(17 * 365 * 24 * 60 * 60)
)

// objectTime is the modification time stampTreeTimes gives a git object, from its id: seconds past objectTimeEpoch from
// the id's first eight bytes, within objectTimeSpan, and nanoseconds from its next eight, so two different objects of
// one size share a time with odds of about one in 2^59.
func objectTime(id string) (time.Time, error) {
	raw, decodeError := hex.DecodeString(id)
	if decodeError != nil || len(raw) < 16 {
		return time.Time{}, fmt.Errorf("%q is no git object id", id)
	}
	var seconds uint64
	var nanoseconds uint64
	for _, value := range raw[:8] {
		seconds = seconds<<8 | uint64(value)
	}
	for _, value := range raw[8:16] {
		nanoseconds = nanoseconds<<8 | uint64(value)
	}
	return objectTimeEpoch.Add(time.Duration(seconds%objectTimeSpan)*time.Second +
		time.Duration(nanoseconds%uint64(time.Second))), nil
}

// treeEntry is one line of `git ls-tree`: its type, blob or tree, its object id, and its path from the tree's root.
type treeEntry struct {
	kind string
	id   string
	path string
}

// stampTreeTimes stamps tree, a checked-out worktree, by its `HEAD`: every tracked file by its blob's id, every
// directory by its tree's id and the root by `HEAD`'s, the directories last and deepest first, since stamping a file
// leaves its directory's time alone but nothing else must touch a directory once it is stamped. A symbolic link and a
// submodule's gitlink are left as they are: the link's own time is the link's, and a submodule is a tree of its own,
// stamped by its own call.
func stampTreeTimes(tree string) error {
	listing, listError := gitOutput(tree, "ls-tree", "-r", "-t", "-z", "--full-tree", "HEAD")
	if listError != nil {
		return fmt.Errorf("listing the tree to stamp: %w", listError)
	}
	rootId, rootError := gitOutput(tree, "rev-parse", "HEAD^{tree}")
	if rootError != nil {
		return fmt.Errorf("reading the tree's root: %w", rootError)
	}
	files, directories := []treeEntry{}, []treeEntry{}
	for _, line := range strings.Split(listing, "\x00") {
		if line == "" {
			continue
		}
		header, path, found := strings.Cut(line, "\t")
		fields := strings.Fields(header)
		if !found || len(fields) != 3 {
			return fmt.Errorf("git ls-tree said %q, not a mode, a type, an id and a path", line)
		}
		mode, kind, id := fields[0], fields[1], fields[2]
		switch {
		case kind == "blob" && mode != "120000":
			files = append(files, treeEntry{kind: kind, id: id, path: path})
		case kind == "tree":
			directories = append(directories, treeEntry{kind: kind, id: id, path: path})
		}
	}
	// Deepest first, so a directory is stamped after every directory inside it.
	slices.SortStableFunc(directories, func(first treeEntry, second treeEntry) int {
		return strings.Count(second.path, "/") - strings.Count(first.path, "/")
	})
	for _, entry := range slices.Concat(files, directories, []treeEntry{{kind: "tree", id: rootId, path: "."}}) {
		when, timeError := objectTime(entry.id)
		if timeError != nil {
			return fmt.Errorf("stamping %s: %w", entry.path, timeError)
		}
		place := filepath.Join(tree, filepath.FromSlash(entry.path))
		if changeError := os.Chtimes(place, when, when); changeError != nil {
			if entry.kind == "tree" && errors.Is(changeError, os.ErrNotExist) {
				// A directory git tracks only through files that aren't checked out has nothing on disk to stamp.
				continue
			}
			return fmt.Errorf("stamping %s: %w", entry.path, changeError)
		}
	}
	return nil
}
