//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A run that reads the table waits while another run holds its lock, and reads the moment the lock is
// released. Without the hold the wait returns at once, which is what makes the wait here mean something.
func TestARunReadingTheTableWaitsForTheWriter(t *testing.T) {
	tablePath := filepath.Join(t.TempDir(), "table.gob")
	if !waitForTableWriter(tablePath, time.Second) {
		t.Fatal("with no lock file the wait did not return as finished")
	}

	release := holdTableLock(tablePath)
	finished := make(chan bool)
	go func() { finished <- waitForTableWriter(tablePath, 10*time.Second) }()
	select {
	case <-finished:
		t.Fatal("the wait returned while the writer still held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	releasedAt := time.Now()
	release()
	select {
	case writerFinished := <-finished:
		if !writerFinished {
			t.Error("the wait reported giving up, though the writer released well inside the bound")
		}
		t.Logf("read %s after the release", time.Since(releasedAt))
	case <-time.After(5 * time.Second):
		t.Fatal("the wait did not return after the writer released the lock")
	}
}

// A writer that never finishes costs the next run its bound and no more, and the run goes ahead.
func TestAStuckWriterCostsTheNextRunItsBound(t *testing.T) {
	tablePath := filepath.Join(t.TempDir(), "table.gob")
	release := holdTableLock(tablePath)
	defer release()
	start := time.Now()
	if waitForTableWriter(tablePath, 100*time.Millisecond) {
		t.Fatal("the wait reported the writer finished while it still held the lock")
	}
	if waited := time.Since(start); waited < 100*time.Millisecond || waited > 5*time.Second {
		t.Errorf("waited %s for a bound of 100ms", waited)
	}
}

// A lone run, with a lock file left by an earlier run and nobody holding it, pays an open and a lock.
func TestALoneRunPaysAlmostNothingToWait(t *testing.T) {
	tablePath := filepath.Join(t.TempDir(), "table.gob")
	holdTableLock(tablePath)()
	if _, err := os.Stat(tableLockPath(tablePath)); err != nil {
		t.Fatalf("the writer left no lock file, so this measures the missing-file path: %v", err)
	}
	const rounds = 1000
	start := time.Now()
	for range rounds {
		if !waitForTableWriter(tablePath, tableWriterWait) {
			t.Fatal("the wait gave up with nobody holding the lock")
		}
	}
	perRun := time.Since(start) / rounds
	t.Logf("%s per lone run", perRun)
	if perRun > time.Millisecond {
		t.Errorf("a lone run's wait costs %s, want microseconds", perRun)
	}
}
