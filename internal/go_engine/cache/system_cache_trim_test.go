package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// writeAgedFile writes size bytes at path, making its directories, and sets its modification time age ago.
func writeAgedFile(test *testing.T, path string, size int, age time.Duration) {
	test.Helper()
	if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := os.WriteFile(path, bytes.Repeat([]byte("t"), size), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	aged := time.Now().Add(-age)
	if timesError := os.Chtimes(path, aged, aged); timesError != nil {
		test.Fatal(timesError)
	}
}

// present reports whether anything is at path, a link as itself.
func present(path string) bool {
	_, statError := os.Lstat(path)
	return statError == nil
}

// trimmedDirectory is a system cache's root and the directory under it a test trims.
func trimmedDirectory(test *testing.T) (string, string) {
	test.Helper()
	root := test.TempDir()
	return root, filepath.Join(root, "go", "standard-library")
}

// A trim deletes a file no run has read for six days and keeps one read an hour ago, touches nothing outside the
// directory it was given, and records when it ran.
func TestATrimDeletesWhatWentUnusedForFiveDays(test *testing.T) {
	test.Parallel()

	root, directory := trimmedDirectory(test)
	unused := filepath.Join(directory, "ab", "unused")
	used := filepath.Join(directory, "cd", "used")
	outside := filepath.Join(root, "outside")
	writeAgedFile(test, unused, 100, 6*24*time.Hour)
	writeAgedFile(test, used, 100, time.Hour)
	writeAgedFile(test, outside, 100, 6*24*time.Hour)

	before := time.Now().Add(-time.Second)
	if notKept := TrimSystemCache(root, directory, SystemCacheSizeCap); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if present(unused) {
		test.Error("a file unused for six days is still there")
	}
	if !present(used) {
		test.Error("a file used an hour ago was deleted")
	}
	if !present(outside) {
		test.Error("a file outside the trimmed directory was deleted")
	}
	lastTrim, recorded := readTrimRecord(filepath.Join(root, trimRecordName))
	if !recorded || lastTrim.Before(before.Truncate(time.Second)) || lastTrim.After(time.Now()) {
		test.Errorf("want the trim recorded as just now, got %v (recorded %v)", lastTrim, recorded)
	}
}

// Past the size cap, a trim deletes the oldest files first until what is left fits, and keeps the newest.
func TestTheSizeCapDeletesTheOldestFirst(test *testing.T) {
	test.Parallel()

	root, directory := trimmedDirectory(test)
	oldest := filepath.Join(directory, "ab", "oldest")
	older := filepath.Join(directory, "cd", "older")
	newer := filepath.Join(directory, "ab", "newer")
	newest := filepath.Join(directory, "ef", "newest")
	writeAgedFile(test, oldest, 100, 4*24*time.Hour)
	writeAgedFile(test, older, 100, 3*24*time.Hour)
	writeAgedFile(test, newer, 100, 2*24*time.Hour)
	writeAgedFile(test, newest, 100, time.Hour)

	if notKept := TrimSystemCache(root, directory, 250); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	for path, want := range map[string]bool{oldest: false, older: false, newer: true, newest: true} {
		if present(path) != want {
			test.Errorf("%s: want present %v under a cap of 250 bytes, got %v", filepath.Base(path), want, !want)
		}
	}
}

// A trim within a day of the last does nothing, even to a file unused for six days, and leaves the record as it was;
// a day after it, the trim runs.
func TestATrimWithinADayOfTheLastDoesNothing(test *testing.T) {
	test.Parallel()

	root, directory := trimmedDirectory(test)
	unused := filepath.Join(directory, "ab", "unused")
	writeAgedFile(test, unused, 100, 6*24*time.Hour)
	recordPath := filepath.Join(root, trimRecordName)
	lastTrim := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	if writeError := os.WriteFile(recordPath, []byte(lastTrim), 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	if notKept := TrimSystemCache(root, directory, SystemCacheSizeCap); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if !present(unused) {
		test.Error("a trim an hour after the last deleted a file")
	}
	if contents, readError := os.ReadFile(recordPath); readError != nil || string(contents) != lastTrim {
		test.Errorf("a trim that did nothing rewrote the record: %q (%v)", contents, readError)
	}

	dayAgo := strconv.FormatInt(time.Now().Add(-25*time.Hour).Unix(), 10)
	if writeError := os.WriteFile(recordPath, []byte(dayAgo), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if notKept := TrimSystemCache(root, directory, SystemCacheSizeCap); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if present(unused) {
		test.Error("a trim a day after the last kept a file unused for six days")
	}
}

// MarkUsed sets the modification time of an entry more than an hour old to now, and leaves one marked within the
// hour as it is, so a warm cache's reads do not each write to the disk.
func TestMarkUsedTouchesOnlyAnEntryOlderThanAnHour(test *testing.T) {
	test.Parallel()

	store := OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	stale := Key{0xAB, 0x01}
	fresh := Key{0xAB, 0x02}
	for _, key := range []Key{stale, fresh} {
		if putError := store.Put(key, []byte("a table")); putError != nil {
			test.Fatal(putError)
		}
	}
	staleTime := time.Now().Add(-2 * time.Hour).Truncate(time.Second)
	freshTime := time.Now().Add(-30 * time.Minute).Truncate(time.Second)
	if timesError := os.Chtimes(store.path(stale), staleTime, staleTime); timesError != nil {
		test.Fatal(timesError)
	}
	if timesError := os.Chtimes(store.path(fresh), freshTime, freshTime); timesError != nil {
		test.Fatal(timesError)
	}

	before := time.Now().Add(-time.Second)
	store.MarkUsed(stale)
	store.MarkUsed(fresh)
	store.MarkUsed(Key{0xCD})
	if information, statError := os.Stat(store.path(stale)); statError != nil || information.ModTime().Before(before) {
		test.Errorf("an entry two hours old was not marked used: %v (%v)", information, statError)
	}
	if information, statError := os.Stat(store.path(fresh)); statError != nil ||
		!information.ModTime().Equal(freshTime) {
		test.Errorf("an entry marked within the hour was marked again: %v (%v)", information, statError)
	}
	if present(store.path(Key{0xCD})) {
		test.Error("marking an entry that is not there made one")
	}
}

// A trim never follows a symbolic link in the directory, nor deletes one: under a cap of nothing, which deletes
// every regular file it reaches, a link to a file and a link to a directory outside stay, and so does what they name.
func TestATrimNeverFollowsASymbolicLink(test *testing.T) {
	test.Parallel()

	root, directory := trimmedDirectory(test)
	outside := test.TempDir()
	outsideFile := filepath.Join(outside, "file")
	outsideDirectoryFile := filepath.Join(outside, "directory", "file")
	writeAgedFile(test, outsideFile, 100, 6*24*time.Hour)
	writeAgedFile(test, outsideDirectoryFile, 100, 6*24*time.Hour)
	inside := filepath.Join(directory, "ab", "inside")
	writeAgedFile(test, inside, 100, 6*24*time.Hour)
	linkToFile := filepath.Join(directory, "ab", "link-to-file")
	linkToDirectory := filepath.Join(directory, "link-to-directory")
	if linkError := os.Symlink(outsideFile, linkToFile); linkError != nil {
		test.Skipf("this system makes no symbolic links: %v", linkError)
	}
	if linkError := os.Symlink(filepath.Dir(outsideDirectoryFile), linkToDirectory); linkError != nil {
		test.Fatal(linkError)
	}

	if notKept := TrimSystemCache(root, directory, 0); notKept != nil {
		test.Fatalf("the trim failed: %s", notKept)
	}
	if present(inside) {
		test.Error("a regular file in the directory was kept under a cap of nothing")
	}
	for _, path := range []string{linkToFile, linkToDirectory, outsideFile, outsideDirectoryFile} {
		if !present(path) {
			test.Errorf("%s was deleted, a link or what one names", path)
		}
	}
}
