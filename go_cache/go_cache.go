// Package go_cache is Go's build cache as its layout stands on disk, and how to delete from it by
// size safely, written once for the programs that trim a cache Go itself bounds only by age. In
// cohere: command/cohere-dev's cache.go, which trims the machine's cache while it holds every token of
// its pool. In Loom: builder's disk.go (trimGoCache, under TrimGoCache at a build-tree's start and
// MakeRoom when a build is short of room). Adamic trims no cache by size.
//
// Go trims its cache only of entries unused for five days, checked once a day. Building and testing
// cohere fills it far faster than that: on 2026-10-03 the default cache regrew 223 GB in nine hours and
// filled the disk (#3sgjy0h, #bdw0dhn). A trim here is Go's own with the age chosen by size instead of
// fixed: it removes the entries used least recently until the cache is under a target, so what a build
// reaches for next survives and nothing has to start cold.
//
// It is in two layers. The first reads the layout: Check knows a cache by its README, and List walks
// its 256 entry subdirectories for action files (-a), output files (-d) and the executable outputs Go
// stores as -d directories, each with its size and the time Go last marked it used. The second is the
// policy over a listing and the delete: Excess is what a cap and a target ask removed, Candidates the
// entries old enough to go, oldest first, Choose the ones a removal would take, Remove one entry after
// checking it is still the one listed, and RemoveOldest and Trim the whole of it. An entry already
// gone, removed by Go's own trim or anyone else, is never an error, in the walk or in the delete.
//
// Neither layer holds a caller's cap, schedule or lock: when a trim runs, how large a cache may grow
// and what keeps builds out while it trims stay each consumer's. What it does hold is the floor under
// every spare, MinimumSpare, since that is Go's arithmetic and not a policy.
//
// It is a module of its own, beside git_tree and memory_limit, so Loom can require it without
// requiring cohere's root module.
package go_cache

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// readmeMarker is how the README the go command writes into every cache it opens begins. A directory
// without it is not a Go build cache, and nothing here lists or removes anything from it.
const readmeMarker = "This directory holds cached build artifacts from the Go build system."

// subdirectories is how many entry subdirectories Go keeps, named 00 to ff by an entry's first byte.
const subdirectories = 256

// Entry is one cache entry: an action file (<xx>/<hash>-a), an output file (<xx>/<hash>-d), or an
// executable output, which Go stores as a -d directory holding the binary.
type Entry struct {
	Path  string
	Bytes int64
	// Used is the entry's modification time, which Go sets when it writes the entry and refreshes when
	// it uses it, but at most once every MinimumSpare.
	Used time.Time
	// Executable is set for an executable output, whose Files are removed by name before its directory.
	Executable bool
	Files      []string
}

// Check says whether a directory is a Go build cache, by the README the go command writes into each one.
func Check(directory string) error {
	readme, readError := os.ReadFile(filepath.Join(directory, "README"))
	if readError != nil || !strings.HasPrefix(string(readme), readmeMarker) {
		return fmt.Errorf("refusing to trim %s: it has no README saying it is a Go build cache", directory)
	}
	return nil
}

// List is every entry of the Go build cache at directory, after Check. Only the 256 entry
// subdirectories are read, and in them only what Go's own trim removes: names ending -a or -d. A link,
// an executable entry holding anything but plain files, and an entry gone before it was measured are
// left out, as is a subdirectory that cannot be read, so the walk never stops partway.
func List(directory string) ([]Entry, error) {
	return list(directory, nil)
}

// list is List, with visiting, when set, run on each entry's path just before the entry is measured, so a
// test can remove one from under the walk.
func list(directory string, visiting func(path string)) ([]Entry, error) {
	if checkError := Check(directory); checkError != nil {
		return nil, checkError
	}
	entries := []Entry{}
	for index := 0; index < subdirectories; index++ {
		subdirectory := filepath.Join(directory, fmt.Sprintf("%02x", index))
		names, readNamesError := readNames(subdirectory)
		if readNamesError != nil {
			continue
		}
		for _, name := range names {
			if !strings.HasSuffix(name, "-a") && !strings.HasSuffix(name, "-d") {
				continue
			}
			path := filepath.Join(subdirectory, name)
			if visiting != nil {
				visiting(path)
			}
			found, measured := readEntry(path)
			if !measured {
				continue
			}
			entries = append(entries, found)
		}
	}
	return entries, nil
}

// Total is the bytes entries hold.
func Total(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}
	return total
}

// readNames lists a directory's names without following anything inside it.
func readNames(directory string) ([]string, error) {
	file, openError := os.Open(directory)
	if openError != nil {
		return nil, openError
	}
	defer file.Close() // Ignored: the directory was only read, so a failed close loses nothing.
	return file.Readdirnames(-1)
}

// readEntry measures one entry, and reports false for one gone, a link, or an executable entry holding a
// nested directory or a link, which is no shape Go writes.
func readEntry(path string) (Entry, bool) {
	information, statError := os.Lstat(path)
	if statError != nil {
		return Entry{}, false
	}
	found := Entry{Path: path, Used: information.ModTime()}
	switch {
	case information.Mode().IsRegular():
		found.Bytes = information.Size()
	case information.IsDir():
		found.Executable = true
		names, readNamesError := readNames(path)
		if readNamesError != nil {
			return Entry{}, false
		}
		for _, name := range names {
			inner, innerStatError := os.Lstat(filepath.Join(path, name))
			if innerStatError != nil || !inner.Mode().IsRegular() {
				return Entry{}, false
			}
			found.Bytes += inner.Size()
			found.Files = append(found.Files, name)
		}
	default:
		return Entry{}, false
	}
	return found, true
}

// gone reports whether an error says a file is not there, which in a cache other removers share is no
// error: Go's own trim, or another trim, may have taken it first.
func gone(fileError error) bool {
	return errors.Is(fileError, fs.ErrNotExist)
}
