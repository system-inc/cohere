package check

import (
	"bytes"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The canonical table holds every entry and variant of the tables it is given, each entry the same by value, its
// analyses in the order of their slots and its digests the ones it had. Two tables that hold the same key, or the same
// variant, are refused, and so is a table that holds nothing.
func TestTheCanonicalTableHoldsTheSameEntriesByValue(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{
		Context:   standardLibraryContext("check tests", goRoot),
		Collected: collected,
	}
	collectingRun := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(collectingRun)
	variant := cache.StandardLibraryVariant{Target: runtime.GOOS + "/" + runtime.GOARCH, CgoEnabled: "1",
		GoVersion: runtime.Version(), Packages: len(collected)}
	written, _, writeError := StandardLibraryTableOf(collected, goRoot, []cache.StandardLibraryVariant{variant})
	if writeError != nil {
		test.Fatalf("writing the table: %v", writeError)
	}
	canonical, canonicalError := CanonicalStandardLibraryTable([][]byte{written}, rules)
	if canonicalError != nil {
		test.Fatalf("writing the canonical table: %v", canonicalError)
	}
	before, after := cache.NewStandardLibraryTable(written), cache.NewStandardLibraryTable(canonical)
	if !reflect.DeepEqual(after.Keys(), before.Keys()) || !reflect.DeepEqual(after.Variants(),
		[]cache.StandardLibraryVariant{variant}) {
		test.Fatalf("want the same keys and the one variant, got %d keys of %d and %+v", len(after.Keys()),
			len(before.Keys()), after.Variants())
	}
	beforeAmong, afterAmong := tableExportData(before), tableExportData(after)
	for _, key := range before.Keys() {
		// Ignored: each key is one of both tables'.
		original, _ := before.Lookup(key)
		rewritten, _ := after.Lookup(key)
		originalDigests, originalError := StandardLibraryDigestsOf(original, beforeAmong)
		rewrittenDigests, rewrittenError := StandardLibraryDigestsOf(rewritten, afterAmong)
		if originalError != nil || rewrittenError != nil || !reflect.DeepEqual(originalDigests, rewrittenDigests) {
			test.Errorf("key %s: want the same digests, got %+v (%v) and %+v (%v)", key, originalDigests,
				originalError, rewrittenDigests, rewrittenError)
		}
		if !bytes.Equal(original.ExportData, rewritten.ExportData) {
			test.Errorf("%s: want its export data kept as it was", originalDigests.PackagePath)
		}
		entry, decodeError := decodeStandardLibraryEntry(rewritten.Entry)
		if decodeError != nil {
			test.Errorf("%s: %v", originalDigests.PackagePath, decodeError)
		}
		originalEntry, originalDecodeError := decodeStandardLibraryEntry(original.Entry)
		if originalDecodeError != nil || len(entry.Analyzers) != len(originalEntry.Analyzers) {
			test.Errorf("%s: want its %d analyses kept, got %d", originalDigests.PackagePath,
				len(originalEntry.Analyzers), len(entry.Analyzers))
		}
	}

	for name, tables := range map[string][][]byte{
		"the same table twice": {written, written},
		"a table of nothing":   {cache.WriteStandardLibraryTable(nil, nil)},
		"no bytes":             {nil},
	} {
		if _, refusedError := CanonicalStandardLibraryTable(tables, rules); refusedError == nil {
			test.Errorf("%s: want it refused", name)
		}
	}
}

// tableExportData is every entry of table, as the packages its entries' export data is read against.
func tableExportData(table *cache.StandardLibraryTable) *StandardLibraryExportData {
	var entries []cache.StandardLibraryEntry
	for _, key := range table.Keys() {
		// Ignored: each key is one of the table's own.
		entry, _ := table.Lookup(key)
		entries = append(entries, entry)
	}
	return StandardLibraryExportDataOf(entries)
}
