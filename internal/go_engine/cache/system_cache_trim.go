package cache

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The system cache is trimmed as Go's build cache is (cmd/go/internal/cache): every dev rebuild is a new engine
// identity, and so a new table for each target it meets, which nothing reads again once the next rebuild runs.
const (
	// trimInterval is how long a trim lasts: a run trims when the last trim was longer ago than this.
	trimInterval = 24 * time.Hour
	// unusedAge is how long a file goes unread before a trim deletes it.
	unusedAge = 5 * 24 * time.Hour
	// markInterval is how stale a file's modification time may be before a read sets it to now. Setting it on
	// every read would write to the disk on every read of a warm cache, and a trim needs only to tell a file read
	// this week from one read last week.
	markInterval = time.Hour
	// trimRecordName is the file at the system cache's root holding when the last trim ran, in Unix seconds.
	trimRecordName = "trim.txt"
)

// SystemCacheSizeCap is how large the files under a trimmed directory may be summed: past it, the oldest go first.
// By guess a table is about 2 MB a target, so a rebuild meeting seven targets adds 14 MB, and the five days of
// unusedAge would rarely reach it.
const SystemCacheSizeCap int64 = 1 << 30

// TrimSystemCache deletes the files under directory, a part of the system cache whose root is root, that no run has
// read for five days, then, while the files left sum past sizeCap, the oldest of them. It trims at most once a day,
// by `trim.txt` at root, which it writes once it has trimmed.
//
// It deletes only regular files, one at a time by name, never a directory, never through a symbolic link, and never
// anything outside directory: a link in it is neither followed nor removed, and directory itself, when a link, is
// said and not walked. Nothing it fails at stops a run: the first failure is the NotKept it returns, nil when the trim
// ran whole or was not due.
func TrimSystemCache(root string, directory string, sizeCap int64) *NotKept {
	now := time.Now()
	recordPath := filepath.Join(root, trimRecordName)
	if lastTrim, recorded := readTrimRecord(recordPath); recorded {
		// A record from the future, from a clock set back, is read as due, so the trim that follows writes it right.
		sinceLastTrim := now.Sub(lastTrim)
		if sinceLastTrim >= 0 && sinceLastTrim < trimInterval {
			return nil
		}
	}

	information, statError := os.Lstat(directory)
	if errors.Is(statError, fs.ErrNotExist) {
		// Nothing was ever written under directory, so there is nothing to trim and no trim to record.
		return nil
	}
	if statError != nil {
		return notKeptBecause(fmt.Sprintf("trimming %s: %v", directory, statError))
	}
	// A file or a link where the directory goes is said, never walked: a regular file there would be deleted as one
	// of the directory's own.
	if !information.IsDir() {
		return notKeptBecause(fmt.Sprintf("trimming %s: it is not a directory", directory))
	}
	files, walkError := regularFilesUnder(directory, recordPath)
	if walkError != nil {
		return notKeptBecause(fmt.Sprintf("trimming %s: %v", directory, walkError))
	}

	var removeErrors []error
	var kept []trimmedFile
	for _, file := range files {
		if now.Sub(file.modified) <= unusedAge {
			kept = append(kept, file)
			continue
		}
		if removeError := os.Remove(file.path); removeError != nil && !errors.Is(removeError, fs.ErrNotExist) {
			removeErrors = append(removeErrors, removeError)
		}
	}

	var size int64
	for _, file := range kept {
		size += file.size
	}
	slices.SortFunc(kept, func(left trimmedFile, right trimmedFile) int {
		return cmp.Or(left.modified.Compare(right.modified), strings.Compare(left.path, right.path))
	})
	for _, file := range kept {
		if size <= sizeCap {
			break
		}
		if removeError := os.Remove(file.path); removeError != nil && !errors.Is(removeError, fs.ErrNotExist) {
			removeErrors = append(removeErrors, removeError)
			continue
		}
		size -= file.size
	}

	// Recorded even when a file could not be removed, as Go records it: a file that cannot be removed now likely
	// cannot be removed in an hour either, and a trim every run would walk the cache every run to find that again.
	writeError := os.WriteFile(recordPath, []byte(strconv.FormatInt(now.Unix(), 10)), 0o644)
	if len(removeErrors) > 0 {
		return notKeptBecause(fmt.Sprintf("trimming %s: %d files could not be removed, the first: %v",
			directory, len(removeErrors), removeErrors[0]))
	}
	if writeError != nil {
		return notKeptBecause(fmt.Sprintf("recording the trim: %v", writeError))
	}
	return nil
}

// readTrimRecord is when the last trim ran, by the record at path. A record missing or not a number is no record,
// so the trim is due.
func readTrimRecord(path string) (time.Time, bool) {
	contents, readError := os.ReadFile(path)
	if readError != nil {
		return time.Time{}, false
	}
	seconds, parseError := strconv.ParseInt(strings.TrimSpace(string(contents)), 10, 64)
	if parseError != nil {
		return time.Time{}, false
	}
	return time.Unix(seconds, 0), true
}

// trimmedFile is a regular file a trim may delete, with what it judges the file by.
type trimmedFile struct {
	path     string
	size     int64
	modified time.Time
}

// regularFilesUnder is every regular file under directory but the trim record. filepath.WalkDir reads each entry
// without following it, so a symbolic link is seen as itself, never as what it names, and is never descended into.
func regularFilesUnder(directory string, recordPath string) ([]trimmedFile, error) {
	var files []trimmedFile
	walkError := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, entryError error) error {
		// An entry removed since its directory was read, by another run's trim or by someone clearing the cache, is
		// gone already.
		if errors.Is(entryError, fs.ErrNotExist) {
			return nil
		}
		if entryError != nil {
			return entryError
		}
		if !entry.Type().IsRegular() || path == recordPath {
			return nil
		}
		// Info is the entry's own, as Lstat reads it, never a link's target's.
		information, informationError := entry.Info()
		if errors.Is(informationError, fs.ErrNotExist) {
			return nil
		}
		if informationError != nil {
			return informationError
		}
		files = append(files, trimmedFile{path: path, size: information.Size(), modified: information.ModTime()})
		return nil
	})
	return files, walkError
}

// markUsed sets the file at path's modification time to now when it is more than markInterval old, so a trim tells a
// file in use from one that is not. A file that cannot be marked is ignored: at worst a trim deletes it, and the next
// run computes what it held, which is a miss, never a wrong read.
func markUsed(path string, now time.Time) {
	information, statError := os.Stat(path)
	if statError != nil || now.Sub(information.ModTime()) < markInterval {
		return
	}
	// Ignored: an unmarked file is trimmed sooner, never read wrong.
	_ = os.Chtimes(path, now, now)
}
