package cache

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Many PutAbsents of one key and one content at once, as the extra builds writing one package's export data by its
// digest do (#5s1x2hg), all succeed and leave that content whole, alone in its directory, the rename being each
// writer's commit.
func TestConcurrentPutAbsentsOfOneContentLeaveOneWholeFile(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	const writers = 16
	content := bytes.Repeat([]byte("export data "), 1<<16)
	putErrors := make([]error, writers)
	var started sync.WaitGroup
	for writer := range writers {
		started.Go(func() { putErrors[writer] = store.PutAbsent(sampleKey, content) })
	}
	started.Wait()
	for writer, putError := range putErrors {
		if putError != nil {
			test.Errorf("PutAbsent %d: %v", writer, putError)
		}
	}
	if got, found := store.Get(sampleKey); !found || !bytes.Equal(got, content) {
		test.Fatalf("want the content whole, got %d bytes (found %v)", len(got), found)
	}
	entries, readError := os.ReadDir(filepath.Dir(store.path(sampleKey)))
	if readError != nil {
		test.Fatal(readError)
	}
	if len(entries) != 1 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		test.Errorf("want the file alone in its directory, got %s", strings.Join(names, ", "))
	}
}

// PutAbsent leaves a whole file of the key alone, writes one that is missing, and writes again one whose bytes were
// damaged at the same size, once a Get has read it as a miss and removed it: damage is never kept by a put that skips.
func TestPutAbsentWritesOnlyWhatIsNotThereWhole(test *testing.T) {
	test.Parallel()

	store := Open(test.TempDir())
	content := []byte("export data by its digest")
	if putError := store.PutAbsent(sampleKey, content); putError != nil {
		test.Fatal(putError)
	}
	path := store.path(sampleKey)
	past := time.Now().Add(-time.Hour)
	if timesError := os.Chtimes(path, past, past); timesError != nil {
		test.Fatal(timesError)
	}
	if putError := store.PutAbsent(sampleKey, content); putError != nil {
		test.Fatal(putError)
	}
	if information, statError := os.Stat(path); statError != nil || !information.ModTime().Equal(past) {
		test.Fatalf("want the whole file left alone, got %v (%v)", information, statError)
	}

	if changeError := changeLastByte(path); changeError != nil {
		test.Fatal(changeError)
	}
	if got, found := store.Get(sampleKey); found {
		test.Fatalf("a damaged file was found: %q", got)
	}
	if _, statError := os.Stat(path); !os.IsNotExist(statError) {
		test.Fatalf("want the damaged file removed by the Get that missed, got %v", statError)
	}
	if putError := store.PutAbsent(sampleKey, content); putError != nil {
		test.Fatal(putError)
	}
	if got, found := store.Get(sampleKey); !found || !bytes.Equal(got, content) {
		test.Fatalf("want the content written again whole, got %q (found %v)", got, found)
	}
}
