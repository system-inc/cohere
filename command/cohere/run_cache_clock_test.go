package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The run cache's clock is a filesystem stamp on a tick of its own: later than a stamp the filesystem gave just
// before it, so whatever the run wrote while preparing is stamped before the clock, never in its tick, where the
// record would refuse the run's own writes (#6cmmpm1). It leaves no file behind, and with no directory to stamp in
// it is an error, never the process's clock.
func TestTheRunCacheClockIsAFilesystemStampOnATickOfItsOwn(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	before, filesystemStampError := filesystemStamp(directory)
	if filesystemStampError != nil {
		test.Fatalf("no stamp could be taken in a fresh temporary directory, so nothing below is about one: %v",
			filesystemStampError)
	}
	clock, filesystemClockError := filesystemClock(directory)
	if filesystemClockError != nil {
		test.Fatalf("no clock in a directory a stamp was just taken in: %v", filesystemClockError)
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
	if missing, missingError := filesystemClock(filepath.Join(directory, "absent")); missingError == nil {
		test.Errorf("with no directory to stamp in, the clock is %s rather than an error", missing)
	}
}

// A run whose clock can't be a filesystem stamp is never recorded, and says why. Its clock would be the process's,
// which lags a Linux stamp, the clock that let a run be recorded over a file added just after it began (#6cmmpm1).
// The run after it, with a stamp, is the one that records, so the one after that replays.
func TestARunWhoseClockIsNoStampIsNeverRecorded(test *testing.T) {
	test.Parallel()
	fixture := newRunCacheFixture(test, buildCohere(test))
	run := func(environment ...string) string {
		test.Helper()
		command := exec.Command(fixture.binary, verboseArguments([]string{"--no-format"})...)
		command.Dir = fixture.root
		command.Env = append(append([]string{"HOME=" + fixture.home}, environment...), withoutHome(os.Environ())...)
		output, outputError := command.CombinedOutput()
		if _, isExit := outputError.(*exec.ExitError); outputError != nil && !isExit {
			test.Fatalf("running cohere: %v\n%s", outputError, output)
		}
		return string(output)
	}
	unstamped := run("COHERE_TEST_NO_CLOCK_STAMP=1")
	if isRunCacheReplay(unstamped) {
		test.Fatalf("the first run of a fresh fixture replayed, so nothing below is about recording:\n%s", unstamped)
	}
	next := run()
	if isRunCacheReplay(next) {
		test.Fatalf("a run whose clock was no filesystem stamp was recorded, and the next run replayed it:\n%s", next)
	}
	if said := unstamped + next; !strings.Contains(said, "its clock is no filesystem stamp") {
		test.Errorf("the run that went unrecorded for its clock never said why:\n%s\n---\n%s", unstamped, next)
	}
	if replayed := run(); !isRunCacheReplay(replayed) {
		test.Errorf("the run after a stamped one did not replay, so the refusal above may be any miss:\n%s", replayed)
	}
}
