package check

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// A kept entry is encoded the first time a build asks for it and never before, once however many ask at the same
// moment, and every ask gets the same bytes; an entry gob refuses is the same error at every ask, wrapping
// errorUnstorable, so the build that asked counts it as no entry.
func TestAKeptEntryIsEncodedOnceWhenABuildFirstAsksForIt(test *testing.T) {
	test.Parallel()

	var encodings atomic.Int64
	contents := lazyEntry(func() (encodedEntry, error) {
		encodings.Add(1)
		return encodedEntry{entry: []byte("entry"), exportData: []byte("export data")}, nil
	})
	if encodings.Load() != 0 {
		test.Fatalf("the entry was encoded %d times before any build asked for it, want 0", encodings.Load())
	}
	var asking sync.WaitGroup
	for range 16 {
		asking.Go(func() {
			entry, exportData, readError := contents()
			if readError != nil || string(entry) != "entry" || string(exportData) != "export data" {
				test.Errorf("an ask read %q and %q with %v, want the entry and its export data", entry, exportData,
					readError)
			}
		})
	}
	asking.Wait()
	if encodings.Load() != 1 {
		test.Errorf("16 asks encoded the entry %d times, want once", encodings.Load())
	}

	refused := lazyEntry(func() (encodedEntry, error) {
		return encodedEntry{}, fmt.Errorf("encoding the entry: %w", errorUnstorable)
	})
	for range 2 {
		if _, _, readError := refused(); !errors.Is(readError, errorUnstorable) {
			test.Errorf("an entry gob refuses read %v, want an error wrapping errorUnstorable", readError)
		}
	}
}
