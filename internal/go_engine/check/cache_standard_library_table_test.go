package check

import (
	"bytes"
	"fmt"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/analysis/passes/printf"
	"golang.org/x/tools/go/gcexportdata"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// The canonical table holds every entry and variant of the tables it is given, each entry the same by value, its
// analyses in the order of their slots and its digests the ones it had, and it restores as the table it came from. Two
// tables that hold the same key, or the same variant, are refused, and so is a table that holds nothing.
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
	runWithSession(test, directory, collecting, rules)
	variant := cache.StandardLibraryVariant{Target: runtime.GOOS + "/" + runtime.GOARCH, CgoEnabled: "1",
		GoVersion: runtime.Version(), Packages: len(collected)}
	written, writeError := StandardLibraryTableOf(collected, goRoot, []cache.StandardLibraryVariant{variant})
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
	for _, key := range before.Keys() {
		// Ignored: each key is one of both tables'.
		original, _ := before.Lookup(key)
		rewritten, _ := after.Lookup(key)
		originalDigests, originalError := StandardLibraryDigestsOf(original)
		rewrittenDigests, rewrittenError := StandardLibraryDigestsOf(rewritten)
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

	fromCanonical := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
		Table:   after,
		Context: standardLibraryContext("check tests", goRoot),
	}}, rules)
	if fromCanonical.result.Cache.FromBinary != len(collected) {
		test.Errorf("want all %d packages restored from the canonical table, got %+v", len(collected),
			fromCanonical.result.Cache)
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

// factStrings are facts as a test compares them, each as its objectpath, its type and its value, sorted, since the
// store holds them in the order the checker handed them over and the table in its own.
func factStrings(restored restoredAnalysis) []string {
	facts := make([]string, 0, len(restored.objectFacts)+len(restored.packageFacts))
	for _, objectFact := range restored.objectFacts {
		facts = append(facts, fmt.Sprintf("%s %T %+v", objectFact.path, objectFact.fact, objectFact.fact))
	}
	for _, packageFact := range restored.packageFacts {
		facts = append(facts, fmt.Sprintf("package %T %+v", packageFact, packageFact))
	}
	slices.Sort(facts)
	return facts
}

// declarationPositions are where export data says each package-level object of the package at path is, by name.
func declarationPositions(test *testing.T, exportData []byte, path string) map[string]string {
	test.Helper()
	fileSet := token.NewFileSet()
	read, readError := gcexportdata.Read(bytes.NewReader(exportData), fileSet, map[string]*types.Package{}, path)
	if readError != nil {
		test.Fatalf("reading the export data of %s: %v", path, readError)
	}
	positions := map[string]string{}
	for _, name := range read.Scope().Names() {
		positions[name] = fileSet.Position(read.Scope().Lookup(name).Pos()).String()
	}
	return positions
}

// A package restored from the table is the package decodeEntry restores from the store's entry it was written from,
// in every part a run reads: each analysis's findings and facts by value, the imports, what the load withheld, the
// output digest, and export data whose every declaration is at the same file and line of this machine's `GOROOT`. The
// run's fact analyzers are printf and ctrlflow, which give fmt, log and os facts of two slots, and net, whose cgo
// files the source loader checks with C's names withheld on linux with cgo on, gives withheld declarations named
// below `GOROOT`.
//
// Not parallel: it sets the process's own `GOOS` and `CGO_ENABLED`, which every go command the load runs reads.
func TestATableEntryRestoresAsTheStoreEntryItWasWrittenFrom(test *testing.T) {
	test.Setenv("GOOS", "linux")
	test.Setenv("CGO_ENABLED", "1")
	directory := test.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/facts\n\ngo 1.24\n",
		"facts.go": "package facts\n\nimport (\n\t\"fmt\"\n\t\"log\"\n\t\"os\"\n\n\t_ \"net\"\n)\n\n" +
			"// Say prints text, or ends the process when there is none.\nfunc Say(text string) {\n" +
			"\tif text == \"\" {\n\t\tlog.Fatal(\"nothing\")\n\t}\n\tfmt.Fprintln(os.Stdout, text)\n}\n",
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	goRoot := goRootOf(test)
	rules := []rule.Rule{
		{Name: rule.Namespace + "printf", Origin: rule.OriginVet, UpstreamName: "printf", Analyzer: printf.Analyzer},
		houseRule(ctrlflow.Analyzer),
	}
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{
		Context:   standardLibraryContext("check tests", goRoot),
		Collected: collected,
	}
	cold := runWithSession(test, directory, collecting, rules)
	written, writeError := StandardLibraryTableOf(collected, goRoot, nil)
	if writeError != nil {
		test.Fatalf("writing the table: %v", writeError)
	}
	canonical, canonicalError := CanonicalStandardLibraryTable([][]byte{written}, rules)
	if canonicalError != nil {
		test.Fatalf("writing the canonical table: %v", canonicalError)
	}
	table := cache.NewStandardLibraryTable(canonical)
	known := cold.session.known
	rename := renameFromGoRootMarker(goRoot)
	facts, withheld := 0, 0
	for key, entry := range collected {
		restoring := &packages.Package{ID: entry.PackagePath, PkgPath: entry.PackagePath}
		fromStore, storeError := decodeWholeEntry(known, restoring, entry.Entry)
		if storeError != nil {
			test.Fatalf("%s: decoding the store's entry: %v", entry.PackagePath, storeError)
		}
		tableEntry, held := table.Lookup(key)
		if !held {
			test.Fatalf("%s: want it in the table", entry.PackagePath)
		}
		fromTable, _, tableError := restoreBinaryEntry(known, restoring, tableEntry, rename)
		if tableError != nil {
			test.Fatalf("%s: restoring the table's entry: %v", entry.PackagePath, tableError)
		}
		if len(fromTable.analyses) != len(fromStore.analyses) {
			test.Errorf("%s: want %d analyses, got %d", entry.PackagePath, len(fromStore.analyses),
				len(fromTable.analyses))
		}
		for analyzer, stored := range fromStore.analyses {
			restored := fromTable.analyses[analyzer]
			if !reflect.DeepEqual(restored.findings, stored.findings) {
				test.Errorf("%s, %s: want the findings %+v, got %+v", entry.PackagePath, analyzer.Name,
					stored.findings, restored.findings)
			}
			if want, got := factStrings(stored), factStrings(restored); !slices.Equal(got, want) {
				test.Errorf("%s, %s: want the facts\n%q\ngot\n%q", entry.PackagePath, analyzer.Name, want, got)
			}
			facts += len(stored.objectFacts) + len(stored.packageFacts)
		}
		withheld += len(fromStore.types.Withheld.Errors) + len(fromStore.types.Withheld.Declarations)
		if !reflect.DeepEqual(fromTable.types.Imports, fromStore.types.Imports) ||
			!reflect.DeepEqual(fromTable.types.Withheld, fromStore.types.Withheld) ||
			fromTable.output != fromStore.output || fromTable.hasOutput != fromStore.hasOutput ||
			fromTable.files != nil || fromStore.files != nil {
			test.Errorf("%s: want the imports, withheld, output and files of the store's entry, got %+v and %+v",
				entry.PackagePath, fromTable.types, fromStore.types)
		}
		if entry.ExportData == nil {
			continue
		}
		exportData, readError := fromTable.types.ReadExportData()
		if readError != nil {
			test.Fatalf("%s: reading the table's export data: %v", entry.PackagePath, readError)
		}
		if want, got := declarationPositions(test, entry.ExportData, entry.PackagePath), declarationPositions(test,
			exportData, entry.PackagePath); !reflect.DeepEqual(got, want) {
			test.Errorf("%s: want every declaration where the store's export data has it", entry.PackagePath)
		}
	}
	if facts == 0 || withheld == 0 {
		test.Fatalf("want facts and withheld declarations among the entries compared, so the comparison read some, "+
			"got %d and %d", facts, withheld)
	}
}
