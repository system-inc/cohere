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

	store := Open(test.TempDir())
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
