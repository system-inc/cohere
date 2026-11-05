//go:build !windows

package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/types/program"
)

// A run that reads the table waits while another run holds its lock, and reads the moment the lock is
// released. Without the hold the wait returns at once, which is what makes the wait here mean something.
func TestARunReadingTheTableWaitsForTheWriter(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	release, taken := holdTableReadLock(directory, time.Second)
	release()
	if !taken {
		test.Fatal("with nobody holding the lock the read lock was not taken")
	}

	releaseWriter := holdTableLock(directory)
	finished := make(chan bool)
	go func() {
		release, taken := holdTableReadLock(directory, 10*time.Second)
		release()
		finished <- taken
	}()
	select {
	case <-finished:
		test.Fatal("the read lock was taken while the writer still held the lock")
	case <-time.After(200 * time.Millisecond):
	}
	releasedAt := time.Now()
	releaseWriter()
	select {
	case taken := <-finished:
		if !taken {
			test.Error("the read lock reported giving up, though the writer released well inside the bound")
		}
		test.Logf("read %s after the release", time.Since(releasedAt))
	case <-time.After(5 * time.Second):
		test.Fatal("the read lock was not taken after the writer released it")
	}
}

// A writer waits for a reader holding the lock shared, so no file is renamed under a read in progress.
func TestAWriterWaitsForAReader(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	releaseReader, taken := holdTableReadLock(directory, time.Second)
	if !taken {
		test.Fatal("the read lock was not taken")
	}
	written := make(chan struct{})
	go func() {
		holdTableLock(directory)()
		close(written)
	}()
	select {
	case <-written:
		test.Fatal("the writer took the lock while a reader held it")
	case <-time.After(200 * time.Millisecond):
	}
	releaseReader()
	select {
	case <-written:
	case <-time.After(5 * time.Second):
		test.Fatal("the writer did not take the lock after the reader released it")
	}
}

// A writer that never finishes costs the next run its bound and no more, and the run goes ahead.
func TestAStuckWriterCostsTheNextRunItsBound(test *testing.T) {
	test.Parallel()
	directory := test.TempDir()
	release := holdTableLock(directory)
	defer release()
	start := time.Now()
	if releaseReader, taken := holdTableReadLock(directory, 100*time.Millisecond); taken {
		releaseReader()
		test.Fatal("the read lock was taken while the writer still held the lock")
	}
	if waited := time.Since(start); waited < 100*time.Millisecond || waited > 5*time.Second {
		test.Errorf("waited %s for a bound of 100ms", waited)
	}
}

// A lone run, with a lock file left by an earlier run and nobody holding it, pays an open and a lock.
// Not parallel: it times a thousand lock round trips against a one millisecond bound, which tests competing
// for the CPU in parallel would blow
func TestALoneRunPaysAlmostNothingToWait(test *testing.T) {
	directory := test.TempDir()
	holdTableLock(directory)()
	if _, statError := os.Stat(tableLockPath(directory)); statError != nil {
		test.Fatalf("the writer left no lock file, so this measures the missing-file path: %v", statError)
	}
	const rounds = 1000
	start := time.Now()
	for range rounds {
		release, taken := holdTableReadLock(directory, tableWriterWait)
		if !taken {
			test.Fatal("the read lock gave up with nobody holding it")
		}
		release()
	}
	perRun := time.Since(start) / rounds
	test.Logf("%s per lone run", perRun)
	if perRun > time.Millisecond {
		test.Errorf("a lone run's lock costs %s, want microseconds", perRun)
	}
}

// The lock makes the table's files whole against each other (#45ekc65). A writer replaces a run and then,
// after a pause, the findings recorded beside it, both naming the same write; a reader reads both under the
// shared lock and must never see one write's run beside another's findings. Run under -race, so the
// goroutines' sharing is checked too. Without the reader's lock the pause between the two renames is where
// it sees them disagree, and this test fails.
// Not parallel: it needs its reader to overlap at least half of 60 timed writes, which tests competing for
// the CPU in parallel can starve
func TestAReaderNeverSeesOneWritersRunBesideAnothersFindings(test *testing.T) {
	directory := test.TempDir()
	identity := cacheTableIdentity()
	const writes = 60
	marker := func(index int) string { return fmt.Sprintf("write %d", index) }

	done := make(chan struct{})
	writerError := make(chan error, 1)
	go func() {
		defer close(done)
		for index := range writes {
			release := holdTableLock(directory)
			table := program.NewCacheTable()
			table.Runs["--no-fix"] = &program.RunCache{Key: marker(index)}
			table.Findings = &program.LintCache{Key: program.HashRuleSet([]string{marker(index)})}
			err := program.WriteCacheTable(directory, table, identity, program.CacheTableSections{Runs: []string{"--no-fix"}}, program.PathAnchor{})
			time.Sleep(2 * time.Millisecond)
			if err == nil {
				err = program.WriteCacheTable(directory, table, identity, program.CacheTableSections{Findings: true}, program.PathAnchor{})
			}
			release()
			if err != nil {
				writerError <- err
				return
			}
			// Runs are never back to back with no gap at all, and flock is not fair: a writer retaking the lock
			// the instant it let go would starve the reader, which polls.
			time.Sleep(3 * time.Millisecond)
		}
	}()

	reads, disagreements := 0, 0
	for reading := true; reading; {
		select {
		case <-done:
			reading = false
		default:
		}
		release, _ := holdTableReadLock(directory, 10*time.Second)
		// Ignored: a table not read, before the first write or for any other reason, holds neither file and is
		// skipped below, and a reader that never reads fails the test on its count of reads.
		table, _ := program.ReadCacheTable(directory, identity, program.CacheTableSections{Runs: []string{"--no-fix"}, Findings: true}, program.PathAnchor{})
		release()
		run := table.Runs["--no-fix"]
		if run == nil || table.Findings == nil {
			continue
		}
		reads++
		if table.Findings.Key != program.HashRuleSet([]string{run.Key}) {
			disagreements++
		}
	}
	select {
	case err := <-writerError:
		test.Fatalf("writing: %v", err)
	default:
	}
	if reads < writes/2 {
		test.Fatalf("only %d reads saw both files across %d writes, so the reader barely overlapped the writer", reads, writes)
	}
	if disagreements > 0 {
		test.Errorf("%d of %d reads saw a run beside findings from another write", disagreements, reads)
	}
}
