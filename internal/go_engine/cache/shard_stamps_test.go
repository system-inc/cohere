package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A shard's stamp moves with every change the store makes to a file of it, a Put of new bytes under a key it holds
// and the removal of a damaged file (forget), and not with MarkUsed, which changes a file's time alone; and a run's
// stamps read each shard once, so a change after the first read is the next run's to see (#b2t9xmf).
func TestAShardsStampMovesWithEveryChangeToItsFiles(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir(), nil)
	if putError := store.Put(sampleKey, []byte("first")); putError != nil {
		test.Fatal(putError)
	}
	stampNow := func() int64 {
		test.Helper()
		reading, read := NewShardStamps(nil).Of(store, ShardOf(sampleKey))
		if !reading.Known || !read {
			test.Fatalf("the shard's stamp was not read (known %v, read %v)", reading.Known, read)
		}
		return reading.Stamp
	}
	first := stampNow()

	// Aged past markInterval, so MarkUsed sets the file's time.
	aged := time.Now().Add(-2 * time.Hour)
	if timesError := os.Chtimes(store.path(sampleKey), aged, aged); timesError != nil {
		test.Fatal(timesError)
	}
	store.MarkUsed(sampleKey)
	if information, statError := os.Stat(store.path(sampleKey)); statError != nil ||
		information.ModTime().Before(time.Now().Add(-time.Minute)) {
		test.Fatalf("MarkUsed did not mark the entry used: %v", statError)
	}
	if marked := stampNow(); marked != first {
		test.Errorf("MarkUsed moved the shard's stamp from %d to %d", first, marked)
	}

	run := NewShardStamps(nil)
	before, _ := run.Of(store, ShardOf(sampleKey))
	if putError := store.Put(sampleKey, []byte("second")); putError != nil {
		test.Fatal(putError)
	}
	rewritten := stampNow()
	if rewritten == first {
		test.Error("a Put of new bytes left the shard's stamp where it was")
	}
	if again, read := run.Of(store, ShardOf(sampleKey)); again != before || read {
		test.Errorf("a run's stamp was read again (%v) or moved within the run: %+v, then %+v", read, before, again)
	}

	if writeError := os.WriteFile(store.path(sampleKey), []byte("damaged"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	// Written in place, as no store writes, so the stamp is set back to show the removal alone moves it.
	directory := filepath.Dir(store.path(sampleKey))
	if timesError := os.Chtimes(directory, time.Unix(0, rewritten), time.Unix(0, rewritten)); timesError != nil {
		test.Fatal(timesError)
	}
	if _, found := store.Get(sampleKey); found {
		test.Fatal("a damaged entry was read")
	}
	if removed := stampNow(); removed == rewritten {
		test.Error("the removal of a damaged entry left the shard's stamp where it was")
	}
}

// A store holding a run's stamps marks a shard it changes moved once the run has read the shard's stamp: a Put, under
// any store of the run, the one the stamp was read through or another over the same directory, as each session opens
// its own, and the removal of a damaged file (forget). The stamp itself stays the run's. A shard changed before the
// run read its stamp is not moved, since the stamp read after holds the change, and a shard of another store, or one
// changed through a store holding other stamps, is not either (#b2t9xmf).
func TestAStoreOfTheRunMarksAShardItChangesMoved(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	run := NewShardStamps(nil)
	reading := Open(root, run)
	writing := Open(root, run)
	elsewhere := Open(test.TempDir(), run)
	notOfTheRun := Open(root, NewShardStamps(nil))
	written, damaged, early := sampleKey, Key{0x12}, Key{0x34}
	for _, key := range []Key{written, damaged, early} {
		if putError := writing.Put(key, []byte("before the run read its stamp")); putError != nil {
			test.Fatal(putError)
		}
	}
	if putError := elsewhere.Put(written, []byte("another store's")); putError != nil {
		test.Fatal(putError)
	}
	readingOf := func(key Key) ShardReading {
		test.Helper()
		got, _ := run.Of(reading, ShardOf(key))
		return got
	}
	if got := readingOf(early); !got.Known || got.Moved {
		test.Errorf("a shard changed only before the run read its stamp reads %+v, want known and not moved", got)
	}
	before := readingOf(written)
	readingOf(damaged)
	if putError := notOfTheRun.Put(written, []byte("a store of another run's")); putError != nil {
		test.Fatal(putError)
	}
	if putError := elsewhere.Put(written, []byte("another store's, again")); putError != nil {
		test.Fatal(putError)
	}
	if got := readingOf(written); got.Moved {
		test.Errorf("a change through a store of another run, or of another directory, moved the shard: %+v", got)
	}
	if putError := writing.Put(written, []byte("after it")); putError != nil {
		test.Fatal(putError)
	}
	if got := readingOf(written); !got.Moved || got.Stamp != before.Stamp {
		test.Errorf("want the shard moved with the stamp the run read, %d, got %+v", before.Stamp, got)
	}
	if writeError := os.WriteFile(writing.path(damaged), []byte("damaged"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if _, found := writing.Get(damaged); found {
		test.Fatal("a damaged entry was read")
	}
	if got := readingOf(damaged); !got.Moved {
		test.Errorf("the removal of a damaged entry left its shard unmoved: %+v", got)
	}
}
