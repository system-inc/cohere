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
// stores as -d directories, each with its size and the time Go last marked it used. Only the names Go
// gives them count, and nothing is reached through a link: whoever writes the cache can plant one, and
// a link in place of a subdirectory once made a trim list and delete files outside the cache (#7jx42ed).
// Every read and every delete goes through an os.Root opened on the cache, so a link swapped in after a
// check can't carry it out either. The second is the
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

// hashDigits is how many hex digits name an entry before its -a or -d: Go's HashSize, 32 bytes
// (cmd/go/internal/cache's hash.go:22), written by fileName (cache.go:117) in lowercase hex.
const hashDigits = 64

// errorNotTheDirectory is a directory that is a link, or no longer the directory it was checked as.
var errorNotTheDirectory = errors.New("not the directory it was checked as")

// afterLstatForTest, when set, is run in openDirectory between the caller's Lstat and the open, so a test can
// swap a link in where only the identity check after the open can catch it. It is nil outside tests.
var afterLstatForTest func(name string)

// Entry is one cache entry: an action file (<xx>/<hash>-a), an output file (<xx>/<hash>-d), or an
// executable output, which Go stores as a -d directory holding the binary.
type Entry struct {
	Path  string
	Bytes int64
	// Used is the entry's modification time, which Go sets when it writes the entry and refreshes when
	// it uses it, but at most once an hour (markUsedLag).
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
// subdirectories are read, each only when it is a real directory and never a link, and in them only the
// names Go gives an entry: the subdirectory's two hex digits leading 64 in lowercase, then -a or -d. A
// link, an executable entry holding anything but plain files, and an entry gone before it was measured
// are left out, as is a subdirectory that cannot be read, so the walk never stops partway.
func List(directory string) ([]Entry, error) {
	return list(directory, nil)
}

// list is List, with visiting, when set, run on each entry's path just before the entry is measured, so a
// test can remove one from under the walk.
func list(directory string, visiting func(path string)) ([]Entry, error) {
	if checkError := Check(directory); checkError != nil {
		return nil, checkError
	}
	cache, openError := os.OpenRoot(directory)
	if openError != nil {
		return nil, fmt.Errorf("listing the Go build cache: %w", openError)
	}
	defer cache.Close() // Ignored: the cache was only read, so a failed close loses nothing.
	entries := []Entry{}
	for index := 0; index < subdirectories; index++ {
		entries = append(entries, listSubdirectory(directory, cache, fmt.Sprintf("%02x", index), visiting)...)
	}
	return entries, nil
}

// listSubdirectory is the entries of one entry subdirectory of the cache at directory, opened in cache, and
// none when it is not a real directory or cannot be read.
func listSubdirectory(directory string, cache *os.Root, subdirectoryName string, visiting func(path string)) []Entry {
	information, statError := cache.Lstat(subdirectoryName)
	if statError != nil {
		return nil
	}
	subdirectory, openError := openDirectory(cache, subdirectoryName, information)
	if openError != nil {
		return nil
	}
	defer subdirectory.Close() // Ignored: the subdirectory was only read, so a failed close loses nothing.
	names, readNamesError := readNames(subdirectory)
	if readNamesError != nil {
		return nil
	}
	entries := []Entry{}
	for _, name := range names {
		if !namesAnEntry(subdirectoryName, name) {
			continue
		}
		path := filepath.Join(directory, subdirectoryName, name)
		if visiting != nil {
			visiting(path)
		}
		found, measured := readEntry(subdirectory, path, name)
		if !measured {
			continue
		}
		entries = append(entries, found)
	}
	return entries
}

// namesAnEntry reports whether name is one Go gives an entry in the subdirectory named subdirectoryName: the
// entry's hash in lowercase hex, whose first byte names the subdirectory, then -a or -d. A trim takes
// nothing else, since anything else in the cache is no entry of Go's.
func namesAnEntry(subdirectoryName, name string) bool {
	if len(name) != hashDigits+len("-a") || name[:len(subdirectoryName)] != subdirectoryName {
		return false
	}
	if suffix := name[hashDigits:]; suffix != "-a" && suffix != "-d" {
		return false
	}
	return lowercaseHex(name[:hashDigits])
}

// namesASubdirectory reports whether name is one of Go's 256 entry subdirectories, 00 to ff.
func namesASubdirectory(name string) bool {
	return len(name) == 2 && lowercaseHex(name)
}

// lowercaseHex reports whether text is hex digits alone, in the lowercase Go writes.
func lowercaseHex(text string) bool {
	for _, character := range text {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

// openDirectory opens name in parent as a root of its own, when information, name's Lstat in parent, says
// it is a real directory, and refuses what it opened unless it is that same directory. OpenRoot follows a
// link that stays inside parent, so a link swapped in after the Lstat would open whatever it points to;
// comparing the two catches it. Whatever is read or removed through the returned root stays in the
// directory that was checked, whatever is renamed or linked in its place afterward.
func openDirectory(parent *os.Root, name string, information fs.FileInfo) (*os.Root, error) {
	if !information.IsDir() {
		return nil, errorNotTheDirectory
	}
	if afterLstatForTest != nil {
		afterLstatForTest(name)
	}
	opened, openError := parent.OpenRoot(name)
	if openError != nil {
		// A link to outside parent swapped in after the Lstat is refused as an escape, which is the same
		// swap as one inside it, not a failure of the cache.
		if current, statError := parent.Lstat(name); statError == nil && !os.SameFile(information, current) {
			return nil, errorNotTheDirectory
		}
		return nil, openError
	}
	openedInformation, statError := opened.Stat(".")
	if statError != nil {
		opened.Close() // Ignored: the root is refused either way, and was only opened.
		return nil, statError
	}
	if !os.SameFile(information, openedInformation) {
		opened.Close() // Ignored: the root is refused either way, and was only opened.
		return nil, errorNotTheDirectory
	}
	return opened, nil
}

// Total is the bytes entries hold.
func Total(entries []Entry) int64 {
	var total int64
	for _, entry := range entries {
		total += entry.Bytes
	}
	return total
}

// readNames lists the names in a directory opened as a root, without following anything inside it.
func readNames(directory *os.Root) ([]string, error) {
	file, openError := directory.Open(".")
	if openError != nil {
		return nil, openError
	}
	defer file.Close() // Ignored: the directory was only read, so a failed close loses nothing.
	return file.Readdirnames(-1)
}

// readEntry measures the entry named name in subdirectory, listed as path, and reports false for one gone, a
// link, or an executable entry holding a nested directory or a link, which is no shape Go writes.
func readEntry(subdirectory *os.Root, path, name string) (Entry, bool) {
	information, statError := subdirectory.Lstat(name)
	if statError != nil {
		return Entry{}, false
	}
	found := Entry{Path: path, Used: information.ModTime()}
	switch {
	case information.Mode().IsRegular():
		found.Bytes = information.Size()
	case information.IsDir():
		found.Executable = true
		executable, openError := openDirectory(subdirectory, name, information)
		if openError != nil {
			return Entry{}, false
		}
		defer executable.Close() // Ignored: the entry was only read, so a failed close loses nothing.
		names, readNamesError := readNames(executable)
		if readNamesError != nil {
			return Entry{}, false
		}
		for _, file := range names {
			inner, innerStatError := executable.Lstat(file)
			if innerStatError != nil || !inner.Mode().IsRegular() {
				return Entry{}, false
			}
			found.Bytes += inner.Size()
			found.Files = append(found.Files, file)
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
