// Package gocache bounds a Go build cache by size, which Go itself never does.
//
// Go trims its cache only of entries unused for five days, checked once a day. Building and testing
// cohere fills it far faster than that: on 2026-10-03 the default cache regrew 223 GB in nine hours and
// filled the disk, and the launcher's private one had reached 310 GB that morning (#3sgjy0h, #bdw0dhn).
// Trim is Go's own trim with the age chosen by size instead of fixed: it removes the entries used least
// recently until the cache is under a target, so what a build reaches for next survives and nothing has to
// start cold.
//
// One caller trims a live cache: cohere-dev, which holds every token of the machine's pool while it does, so
// no build it can see is reading what it removes. The launcher trimmed the default cache too, holding
// nothing, and was removed: a second path deleting from the live cache without the pool's lock is how the
// cache was trimmed under running gates on 2026-10-06 (#cpxc2d7, #d0x1fhp). TestOnlyCohereDevTrims holds it
// to one.
package gocache

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// readmeMarker is how the README the go command writes into every cache it opens begins. A directory
// without it is not a Go build cache, and Trim refuses to remove anything from it.
const readmeMarker = "This directory holds cached build artifacts from the Go build system."

// InUseWindow is how recently used an entry may be and still be one a running build is about to read, and
// no entry inside it is ever removed.
//
// Go marks an entry used by touching its modification time, but at most once an hour, so an entry a build
// looked up a minute ago can carry a time 59 minutes old; the last half hour covers that build still
// running. Inside the hour, age cannot tell an entry nobody wants from one a build is reading, and the
// disk's access times cannot either, since a read updates them at most once a day. Removing one a build
// is reading fails that build with "could not import ... no such file", which reads as a compile error:
// the #3sgjy0h gate did exactly that when it trimmed young entries to hold a burst under the cap. So a
// burst can stand above the cap by what it wrote inside the window, and it never breaks a build.
const InUseWindow = 90 * time.Minute

// Limit is the size a cache may reach before it is trimmed, the size a trim brings it down to, and how
// recently used an entry may be and still be spared. Trimming below the cap means a cache growing steadily
// is trimmed once in a while rather than on every look.
type Limit struct {
	Cap    int64
	Target int64
	// Spare is how recently used an entry may be and never be removed: InUseWindow for a trim that runs
	// beside builds it cannot see, less for one that has kept them out, as cohere-dev's does by holding
	// every token of the pool while it trims.
	Spare time.Duration
}

// Trimmed is what one trim found and removed.
type Trimmed struct {
	Directory string
	Before    int64
	After     int64
	Removed   int
	// InUse is the bytes of entries used within the limit's Spare window, which the trim did not remove.
	// When After is over the cap, this is why.
	InUse int64
}

// entry is one cache entry: an action file (-a), an output file (-d), or an executable output, which Go
// stores as a -d directory holding the binary.
type entry struct {
	path  string
	bytes int64
	used  time.Time
	files []string // the files inside an executable entry, removed by name before its directory
}

// Trim removes a Go build cache's least recently used entries once it is over limit.Cap, until it is at or
// under limit.Target or only entries used within limit.Spare are left.
//
// Only the 256 entry subdirectories are touched, and in them only what Go's own trim removes: names
// ending -a or -d. Every removal is one named file. An entry already gone is skipped, since the go command
// or another trim may have removed it first.
func Trim(directory string, limit Limit, now time.Time) (Trimmed, error) {
	trimmed := Trimmed{Directory: directory}
	if checkError := Check(directory); checkError != nil {
		return trimmed, checkError
	}

	inUseSince := now.Add(-limit.Spare)
	entries := []entry{}
	for index := 0; index < 256; index++ {
		subdirectory := filepath.Join(directory, fmt.Sprintf("%02x", index))
		names, readNamesError := readNames(subdirectory)
		if readNamesError != nil {
			continue
		}
		for _, name := range names {
			if !strings.HasSuffix(name, "-a") && !strings.HasSuffix(name, "-d") {
				continue
			}
			found, ok := readEntry(filepath.Join(subdirectory, name))
			if !ok {
				continue
			}
			trimmed.Before += found.bytes
			if found.used.After(inUseSince) {
				trimmed.InUse += found.bytes
				continue
			}
			entries = append(entries, found)
		}
	}
	trimmed.After = trimmed.Before
	if trimmed.Before <= limit.Cap {
		return trimmed, nil
	}

	sort.Slice(entries, func(left, right int) bool { return entries[left].used.Before(entries[right].used) })
	for _, candidate := range entries {
		if trimmed.After <= limit.Target {
			break
		}
		if removeEntryError := removeEntry(candidate); removeEntryError != nil {
			return trimmed, removeEntryError
		}
		trimmed.After -= candidate.bytes
		trimmed.Removed++
	}
	return trimmed, nil
}

// Check says whether a directory is a Go build cache, by the README the go command writes into each one.
func Check(directory string) error {
	readme, readError := os.ReadFile(filepath.Join(directory, "README"))
	if readError != nil || !strings.HasPrefix(string(readme), readmeMarker) {
		return fmt.Errorf("refusing to trim %s: it has no README saying it is a Go build cache", directory)
	}
	return nil
}

// readNames lists a directory's names without following anything inside it.
func readNames(directory string) ([]string, error) {
	file, openError := os.Open(directory)
	if openError != nil {
		return nil, openError
	}
	defer file.Close()
	return file.Readdirnames(-1)
}

// readEntry measures one entry. An executable entry is a directory of plain files, and anything else in
// it, a nested directory or a link, makes it one Trim leaves alone.
func readEntry(path string) (entry, bool) {
	information, statError := os.Lstat(path)
	if statError != nil {
		return entry{}, false
	}
	found := entry{path: path, used: information.ModTime()}
	switch {
	case information.Mode().IsRegular():
		found.bytes = information.Size()
	case information.IsDir():
		names, readNamesError := readNames(path)
		if readNamesError != nil {
			return entry{}, false
		}
		for _, name := range names {
			inner, statError := os.Lstat(filepath.Join(path, name))
			if statError != nil || !inner.Mode().IsRegular() {
				return entry{}, false
			}
			found.bytes += inner.Size()
			found.files = append(found.files, name)
		}
	default:
		return entry{}, false
	}
	return found, true
}

// removeEntry removes one entry, an executable entry's files by name and then its emptied directory.
func removeEntry(candidate entry) error {
	for _, name := range candidate.files {
		if removeError := os.Remove(filepath.Join(candidate.path, name)); removeError != nil && !os.IsNotExist(removeError) {
			return fmt.Errorf("trimming the Go build cache: %w", removeError)
		}
	}
	if removeError := os.Remove(candidate.path); removeError != nil && !os.IsNotExist(removeError) {
		return fmt.Errorf("trimming the Go build cache: %w", removeError)
	}
	return nil
}
