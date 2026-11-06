package go_cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newCache makes a directory in Go's cache layout, README included.
func newCache(test *testing.T) string {
	test.Helper()
	directory := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(directory, "README"),
		[]byte(readmeMarker+"\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	return directory
}

// put writes a cache file of the given size, last used at the given time.
func put(test *testing.T, directory, name string, bytes int, used time.Time) string {
	test.Helper()
	path := filepath.Join(directory, name)
	if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}
	if writeError := os.WriteFile(path, []byte(strings.Repeat("x", bytes)), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if changeTimesError := os.Chtimes(path, used, used); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	return path
}

func exists(path string) bool {
	_, statError := os.Lstat(path)
	return statError == nil
}

// A directory that does not say it is a Go build cache is never trimmed, however large.
func TestADirectoryWithoutGosReadmeIsRefused(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	file := put(test, directory, "ab/0000ab-d", 4096, time.Now().Add(-48*time.Hour))
	if _, trimError := Trim(
		directory,
		Limit{Cap: 1, Target: 0, Spare: InUseWindow},
		time.Now(),
	); trimError == nil || !strings.Contains(trimError.Error(), "refusing to trim") {
		test.Fatalf("a directory with no README was trimmed: %v", trimError)
	}
	if !exists(file) {
		test.Fatal("a refused trim removed a file")
	}
}

// Over its cap, a cache loses its least recently used entries until it is at its target, and keeps the
// rest, everything that is not an entry, and what is used now.
func TestTheLeastRecentlyUsedEntriesGoFirstDownToTheTarget(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	now := time.Now()
	oldest := put(test, directory, "00/00aa-d", 400, now.Add(-72*time.Hour))
	older := put(test, directory, "01/01aa-a", 400, now.Add(-48*time.Hour))
	old := put(test, directory, "02/02aa-d", 400, now.Add(-24*time.Hour))
	recent := put(test, directory, "03/03aa-d", 400, now.Add(-time.Minute))
	notAnEntry := put(test, directory, "04/testexpand", 400, now.Add(-96*time.Hour))
	trimFile := put(test, directory, "trim.txt", 400, now.Add(-96*time.Hour))

	// 1,600 bytes of entries against a cap of 1,000 and a target of 900: the two oldest go.
	trimmed, trimError := Trim(directory, Limit{Cap: 1000, Target: 900, Spare: InUseWindow}, now)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Before != 1600 || trimmed.After != 800 || trimmed.Removed != 2 || trimmed.InUse != 400 {
		test.Fatalf("trimmed %+v, want 1600 to 800 bytes in 2 entries, with 400 in use", trimmed)
	}
	for _, gone := range []string{oldest, older} {
		if exists(gone) {
			test.Errorf("%s survived", gone)
		}
	}
	for _, kept := range []string{old, recent, notAnEntry, trimFile, filepath.Join(directory, "README")} {
		if !exists(kept) {
			test.Errorf("%s was removed", kept)
		}
	}

	// Under its cap, nothing is touched.
	trimmed, trimError = Trim(directory, Limit{Cap: 1000, Target: 0, Spare: InUseWindow}, now)
	if trimError != nil || trimmed.Removed != 0 || trimmed.Before != 800 {
		test.Fatalf("a cache under its cap was trimmed: %+v, %v", trimmed, trimError)
	}
}

// Entries used within the in-use window are what a running build may be reading, so no trim removes them,
// even with the cache over its cap and nothing else to take. The #3sgjy0h gate removed young entries to
// hold a burst under the cap, and a live `go test` failed with "could not import ... no such file".
func TestEntriesInUseAreNeverRemoved(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	now := time.Now()
	stale := put(test, directory, "00/00aa-d", 400, now.Add(-InUseWindow-time.Minute))
	inUse := []string{
		put(test, directory, "01/01aa-d", 400, now.Add(-InUseWindow+time.Minute)),
		put(test, directory, "02/02aa-d", 400, now.Add(-30*time.Minute)),
		put(test, directory, "03/03aa-a", 400, now),
	}

	trimmed, trimError := Trim(directory, Limit{Cap: 1000, Target: 100, Spare: InUseWindow}, now)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed != 1 || trimmed.InUse != 1200 || trimmed.After != 1200 {
		test.Fatalf("trimmed %+v, want only the stale entry removed and 1200 bytes in use kept over the cap", trimmed)
	}
	if exists(stale) {
		test.Error("the entry unused for longer than the window survived")
	}
	for _, path := range inUse {
		if !exists(path) {
			test.Errorf("%s, used within the window, was removed", path)
		}
	}
}

// A trim that has kept every build out spares only its short window: the entries a build read half an hour
// ago go when the cache is over its cap, and the last minutes' writes stay (#jc6ca7r).
func TestAShortSpareWindowReachesRecentlyUsedEntries(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	now := time.Now()
	older := put(test, directory, "00/00aa-d", 400, now.Add(-30*time.Minute))
	recent := put(test, directory, "01/01aa-d", 400, now.Add(-20*time.Minute))
	justWritten := put(test, directory, "02/02aa-a", 400, now.Add(-2*time.Minute))

	trimmed, trimError := Trim(directory, Limit{Cap: 1000, Target: 500, Spare: 10 * time.Minute}, now)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed != 2 || trimmed.After != 400 || trimmed.InUse != 400 {
		test.Fatalf("trimmed %+v, want the two entries older than the window removed, oldest first", trimmed)
	}
	if exists(older) || exists(recent) || !exists(justWritten) {
		test.Errorf("after the trim: 30 minutes old %v, 20 minutes old %v, 2 minutes old %v; want only the last kept",
			exists(older), exists(recent), exists(justWritten))
	}
}

// An executable entry is a directory holding the binary. It is measured whole and removed file by file,
// and one holding anything but plain files is left alone.
func TestAnExecutableEntryIsRemovedByName(test *testing.T) {
	test.Parallel()
	directory := newCache(test)
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	binary := put(test, directory, "00/00aa-d/cohere", 2000, old)
	if changeTimesError := os.Chtimes(filepath.Dir(binary), old, old); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}
	nested := put(test, directory, "01/01aa-d/inner/file", 2000, old)
	if changeTimesError := os.Chtimes(filepath.Dir(filepath.Dir(nested)), old, old); changeTimesError != nil {
		test.Fatal(changeTimesError)
	}

	trimmed, trimError := Trim(directory, Limit{Cap: 1000, Target: 0, Spare: InUseWindow}, now)
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed != 1 || trimmed.Before != 2000 {
		test.Fatalf("trimmed %+v, want the one executable entry of 2000 bytes", trimmed)
	}
	if exists(filepath.Dir(binary)) {
		test.Error("the executable entry survived")
	}
	if !exists(nested) {
		test.Error("an entry holding a directory was removed")
	}
}

// Positive control on a real cache: the go command fills it, a trim to nothing removes every entry, and
// the go command opens the trimmed cache and builds again with no error.
func TestTheGoCommandBuildsFromATrimmedCache(test *testing.T) {
	test.Parallel()
	if _, lookPathError := exec.LookPath("go"); lookPathError != nil {
		test.Skip("no go command")
	}
	cache := test.TempDir()
	module := test.TempDir()
	if writeError := os.WriteFile(
		filepath.Join(module, "go.mod"),
		[]byte("module trimmed\n\ngo 1.21\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	if writeError := os.WriteFile(
		filepath.Join(module, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	build := func() {
		test.Helper()
		command := exec.Command("go", "build", "-o", filepath.Join(test.TempDir(), "trimmed"), ".")
		command.Dir = module
		command.Env = append(os.Environ(), "GOCACHE="+cache, "GOFLAGS=", "GOWORK=off")
		if output, outputError := command.CombinedOutput(); outputError != nil {
			test.Fatalf("go build: %v: %s", outputError, output)
		}
	}
	build()

	// A day from now nothing is in use, so a target of zero takes every entry.
	trimmed, trimError := Trim(cache, Limit{Cap: 0, Target: 0, Spare: InUseWindow}, time.Now().Add(24*time.Hour))
	if trimError != nil {
		test.Fatal(trimError)
	}
	if trimmed.Removed == 0 || trimmed.After != 0 {
		test.Fatalf("trimmed %+v, want every entry gone", trimmed)
	}
	build()
}
