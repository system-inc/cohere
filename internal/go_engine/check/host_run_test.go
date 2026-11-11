package check

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

// A kept entry is encoded the first time a build asks for it and never before, once however many ask at the same
// moment, and its export data is written only when a load reads it, once however many read at once; every ask gets the
// same bytes. An entry gob refuses is the same error at every ask, wrapping errorUnstorable, so the build that asked
// counts it as no entry.
func TestAKeptEntryIsEncodedOnceWhenABuildFirstAsksForIt(test *testing.T) {
	test.Parallel()

	var encodings, exportWrites atomic.Int64
	contents := lazyEntry(func() (encodedEntry, error) {
		encodings.Add(1)
		return encodedEntry{entry: []byte("entry")}, nil
	}, func() ([]byte, error) {
		exportWrites.Add(1)
		return []byte("export data"), nil
	})
	if encodings.Load() != 0 || exportWrites.Load() != 0 {
		test.Fatalf("before any build asked, the entry was encoded %d times and its export data written %d, want 0",
			encodings.Load(), exportWrites.Load())
	}
	readers := make([]func() ([]byte, error), 16)
	var asking sync.WaitGroup
	for index := range readers {
		asking.Go(func() {
			entry, readExportData, readError := contents()
			if readError != nil || string(entry) != "entry" {
				test.Errorf("an ask read %q with %v, want the entry", entry, readError)
			}
			readers[index] = readExportData
		})
	}
	asking.Wait()
	if encodings.Load() != 1 || exportWrites.Load() != 0 {
		test.Errorf("16 asks encoded the entry %d times and wrote its export data %d, want once and never",
			encodings.Load(), exportWrites.Load())
	}
	var reading sync.WaitGroup
	for _, readExportData := range readers {
		reading.Go(func() {
			if exportData, readError := readExportData(); readError != nil || string(exportData) != "export data" {
				test.Errorf("a load read %q with %v, want the export data", exportData, readError)
			}
		})
	}
	reading.Wait()
	if exportWrites.Load() != 1 {
		test.Errorf("16 loads reading wrote the export data %d times, want once", exportWrites.Load())
	}

	refused := lazyEntry(func() (encodedEntry, error) {
		return encodedEntry{}, fmt.Errorf("encoding the entry: %w", errorUnstorable)
	}, func() ([]byte, error) { return nil, nil })
	for range 2 {
		if _, _, readError := refused(); !errors.Is(readError, errorUnstorable) {
			test.Errorf("an entry gob refuses read %v, want an error wrapping errorUnstorable", readError)
		}
	}
}
