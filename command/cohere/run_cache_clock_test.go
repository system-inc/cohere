package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The run cache's clock is a filesystem stamp on a tick of its own: later than a stamp the filesystem gave just
// before it, so whatever the run wrote while preparing is stamped before the clock, never in its tick, where the
// record would refuse the run's own writes (#6cmmpm1). It leaves no file behind, and with no directory to stamp in
// it is the fallback.
func TestTheRunCacheClockIsAFilesystemStampOnATickOfItsOwn(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	before, stamped := filesystemStamp(directory)
	if !stamped {
		test.Fatal("no stamp could be taken in a fresh temporary directory, so nothing below is about one")
	}
	fallback := time.Unix(1, 0)
	clock := filesystemClock(directory, fallback)
	if clock.Equal(fallback) {
		test.Fatal("the clock fell back in a directory a stamp was just taken in")
	}
	if !clock.After(before) {
		test.Errorf("the clock %s is not later than the stamp taken before it, %s", clock, before)
	}
	written := filepath.Join(directory, "written-after")
	if writeError := os.WriteFile(written, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	information, statError := os.Stat(written)
	if statError != nil {
		test.Fatal(statError)
	}
	if information.ModTime().Before(clock) {
		test.Errorf("a file written after the clock is stamped %s, before the clock %s", information.ModTime(), clock)
	}
	entries, readDirectoryError := os.ReadDir(directory)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	if len(entries) != 1 {
		test.Errorf("the clock left files behind: %d entries where only written-after belongs", len(entries))
	}
	if missing := filesystemClock(filepath.Join(directory, "absent"), fallback); !missing.Equal(fallback) {
		test.Errorf("with no directory to stamp in, the clock is %s, not the fallback", missing)
	}
}
