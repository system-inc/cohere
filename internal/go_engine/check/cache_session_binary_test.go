package check

import (
	"bytes"
	"encoding/gob"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// countedFact is the fact countingFactRule's analyzer learns, which it never exports: an analyzer with a fact type
// runs on every package below a root, as a run's fact analyzers do on the standard library.
type countedFact struct {
	Counted bool
}

// AFact marks countedFact a fact.
func (*countedFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// countingFactRule is a rule whose analyzer learns facts, so it runs on every package below the packages analyzed,
// and counts its runs by package path in runs. Its one option, mode, changes nothing it does, only its flags.
func countingFactRule(runs *sync.Map) rule.Rule {
	analyzer := &analysis.Analyzer{
		Name:      "countingFacts",
		Doc:       "counts its runs on each package",
		FactTypes: []analysis.Fact{new(countedFact)},
		Run: func(pass *analysis.Pass) (any, error) {
			count, _ := runs.LoadOrStore(pass.Pkg.Path(), new(atomic.Int32))
			if counter, isCounter := count.(*atomic.Int32); isCounter {
				counter.Add(1)
			}
			return nil, nil
		},
	}
	analyzer.Flags.String("mode", "first", "changes nothing it does")
	return houseRule(analyzer)
}

// standardLibraryModule writes a module of the test's own that imports three standard-library packages, and returns
// its directory.
func standardLibraryModule(test *testing.T) string {
	test.Helper()
	directory := test.TempDir()
	files := map[string]string{
		"go.mod": "module example.com/standard\n\ngo 1.24\n",
		"standard.go": "package standard\n\nimport (\n\t\"errors\"\n\t\"strings\"\n\t\"unicode/utf8\"\n)\n\n" +
			"// Shout is text in capitals, or an error for bytes that are not text.\n" +
			"func Shout(text string) (string, error) {\n\tif !utf8.ValidString(text) {\n" +
			"\t\treturn \"\", errors.New(\"not text\")\n\t}\n\treturn strings.ToUpper(text), nil\n}\n",
	}
	for name, contents := range files {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	return directory
}

// goRootOf is go env's `GOROOT`, failing the test when go env fails.
func goRootOf(test *testing.T) string {
	test.Helper()
	output, runError := exec.Command("go", "env", "GOROOT").Output()
	if runError != nil {
		test.Fatalf("reading GOROOT with go env: %v", runError)
	}
	return strings.TrimSpace(string(output))
}

// standardLibraryContext is what a test's standard-library keys share, under stamp, with goRoot.
func standardLibraryContext(stamp string, goRoot string) cache.StandardLibraryContext {
	return cache.StandardLibraryContext{
		FormatVersion: CacheFormatVersion,
		ReleaseStamp:  stamp,
		Build:         []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH},
		Goroot:        goRoot,
	}
}

// standardRuns is how many times runs counted an analysis of a standard-library package, whose path has no dot in
// its first element.
func standardRuns(runs *sync.Map) int {
	total := 0
	runs.Range(func(path any, count any) bool {
		packagePath, isPath := path.(string)
		first, _, _ := strings.Cut(packagePath, "/")
		if counter, isCounter := count.(*atomic.Int32); isPath && isCounter && !strings.Contains(first, ".") {
			total += int(counter.Load())
		}
		return true
	})
	return total
}

// A cached run collects every standard-library package it saves, and the table written from them restores each one
// in a run with no store at all: nothing analyzes a package restored from the table, nothing parses one, the store
// counts stay at 0 and the result is a run's without the cache. strings' types come from its export data, renamed to
// this machine's `GOROOT`, so a position of strings.ToUpper names the local file; a table read without the rename
// names `$GOROOT/src/strings`.
func TestAStandardLibraryPackageFromTheBinaryIsNeverAnalyzedOrParsed(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{
		Context:   standardLibraryContext("check tests", goRoot),
		Collected: collected,
	}
	cold := runWithSession(test, directory, collecting, rules)
	if len(collected) == 0 || len(collected) >= cold.result.Cache.Written {
		test.Fatalf("want every standard-library package written collected, and the module not, got %d of %+v",
			len(collected), cold.result.Cache)
	}
	table, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}

	runs.Clear()
	plainLoad, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	plain, runError := Run(plainLoad.Packages, rules, Options{})
	if runError != nil {
		test.Fatalf("running without the cache: %v", runError)
	}
	if standardRuns(runs) == 0 {
		test.Fatal("want the fact analyzer run on the standard library without the cache")
	}

	runs.Clear()
	fromBinary := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
		Table:   cache.NewStandardLibraryTable(table),
		Context: standardLibraryContext("check tests", goRoot),
	}}, rules)
	if counted := standardRuns(runs); counted != 0 {
		test.Errorf("want nothing analyzed on a standard-library package from the binary, got %d runs", counted)
	}
	if want := (CacheCounts{FromBinary: len(collected)}); !reflect.DeepEqual(fromBinary.result.Cache, want) {
		test.Errorf("want every collected package from the binary and no store count, got %+v", fromBinary.result.Cache)
	}
	if fromBinary.loaded.FilesParsed != 1 {
		test.Errorf("want the module's one file parsed alone, got %d", fromBinary.loaded.FilesParsed)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(fromBinary.result), withoutTimesOrCache(plain)) {
		test.Errorf("from the binary\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(fromBinary.result),
			withoutTimesOrCache(plain))
	}
	var restoredStrings *packages.Package
	packages.Visit(fromBinary.loaded.Packages, nil, func(visited *packages.Package) {
		if visited.PkgPath == "strings" {
			restoredStrings = visited
		}
	})
	if restoredStrings == nil || restoredStrings.Types == nil {
		test.Fatal("want strings loaded with its types")
	}
	position := restoredStrings.Fset.Position(restoredStrings.Types.Scope().Lookup("ToUpper").Pos())
	if want := filepath.Join(goRoot, "src", "strings") + string(filepath.Separator); !strings.HasPrefix(
		position.Filename, want) {
		test.Errorf("want strings.ToUpper at a file below %s, got %s", want, position)
	}
}

// A table written under one release stamp restores nothing under another, nor under another build, nor in a run whose
// fact analyzers' options are not the table's: each key differs, so every standard-library package computes.
func TestAStandardLibraryTableUnderAnotherKeyRestoresNothing(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{
		Context:   standardLibraryContext("release one", goRoot),
		Collected: collected,
	}
	runWithSession(test, directory, collecting, rules)
	table, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	readUnder := func(context cache.StandardLibraryContext) (CacheCounts, int) {
		test.Helper()
		runs.Clear()
		read := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
			Table:   cache.NewStandardLibraryTable(table),
			Context: context,
		}}, rules)
		return read.result.Cache, standardRuns(runs)
	}
	if counts, analyzed := readUnder(standardLibraryContext("release one", goRoot)); counts.FromBinary == 0 ||
		analyzed != 0 {
		test.Fatalf("want the table read under its own key, got %+v and %d runs", counts, analyzed)
	}
	otherBuild := standardLibraryContext("release one", goRoot)
	otherBuild.Build = []string{"GOOS=plan9", "GOARCH=" + runtime.GOARCH}
	for name, context := range map[string]cache.StandardLibraryContext{
		"another stamp": standardLibraryContext("release two", goRoot),
		"another build": otherBuild,
	} {
		if counts, analyzed := readUnder(context); counts.FromBinary != 0 || analyzed == 0 {
			test.Errorf("%s: want nothing from the binary and the standard library analyzed, got %+v and %d runs",
				name, counts, analyzed)
		}
	}
	// Last, since the option stays set on the rule's analyzer.
	if setError := rules[0].Analyzer.Flags.Set("mode", "second"); setError != nil {
		test.Fatal(setError)
	}
	if counts, analyzed := readUnder(standardLibraryContext("release one", goRoot)); counts.FromBinary != 0 ||
		analyzed == 0 {
		test.Errorf("another fact option: want nothing from the binary and the standard library analyzed, got %+v and "+
			"%d runs", counts, analyzed)
	}
}

// The table is written for any machine: an entry's withheld files are renamed to `$GOROOT` and its findings left out,
// while an entry naming a file outside `GOROOT`, or holding the walk of its files, is refused.
func TestAStandardLibraryTableIsWrittenForAnyMachine(test *testing.T) {
	test.Parallel()

	goRoot := filepath.Join(string(filepath.Separator), "usr", "local", "go")
	encoded := func(entry cacheEntry) []byte {
		test.Helper()
		var written bytes.Buffer
		if encodeError := gob.NewEncoder(&written).Encode(entry); encodeError != nil {
			test.Fatal(encodeError)
		}
		return written.Bytes()
	}
	inRoot := filepath.Join(goRoot, "src", "net", "cgo_unix.go")
	portable := cacheEntry{
		Version: CacheFormatVersion,
		Analyzers: map[string]storedAnalysis{"printf": {Findings: []Finding{{Rule: "printf", File: inRoot,
			Line: 3}}}},
		Withheld: load.PackageWithheld{
			Errors:       []load.WithheldError{{File: inRoot, Line: 3, Column: 2, Message: "withheld"}},
			Declarations: []string{inRoot + ":7:6"},
		},
	}
	key := cache.Key{0x01}
	written, writeError := StandardLibraryTableOf(map[cache.Key]StandardLibraryRunEntry{
		key: {PackagePath: "net", Entry: encoded(portable)},
	}, goRoot, nil)
	if writeError != nil {
		test.Fatalf("writing a portable entry: %v", writeError)
	}
	read, held := cache.NewStandardLibraryTable(written).Lookup(key)
	if !held {
		test.Fatal("want the entry in the table")
	}
	entry, decodeError := decodeStandardLibraryEntry(read.Entry)
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	marked := "$GOROOT/src/net/cgo_unix.go"
	want := load.PackageWithheld{
		Errors:       []load.WithheldError{{File: marked, Line: 3, Column: 2, Message: "withheld"}},
		Declarations: []string{marked + ":7:6"},
	}
	if !reflect.DeepEqual(entry.Withheld, want) || entry.PackagePath != "net" || len(entry.Analyzers) != 1 ||
		entry.Analyzers[0].Slot != "printf" || len(entry.Analyzers[0].Analysis.Findings) != 0 {
		test.Errorf("want net's entry, its withheld files renamed and no finding, got %+v", entry)
	}

	outside := portable
	outside.Withheld = load.PackageWithheld{Declarations: []string{filepath.Join(string(filepath.Separator), "opt",
		"other-go", "src", "net", "cgo_unix.go") + ":7:6"}}
	walked := portable
	walked.Files = []storedFile{{Name: inRoot, Nodes: 1}}
	for name, refused := range map[string]cacheEntry{"outside GOROOT": outside, "with its files' walk": walked} {
		if _, refusedError := StandardLibraryTableOf(map[cache.Key]StandardLibraryRunEntry{
			key: {PackagePath: "net", Entry: encoded(refused)},
		}, goRoot, nil); refusedError == nil {
			test.Errorf("%s: want the entry refused", name)
		}
	}
}

// filesKeyed is every distinct file of the packages loaded reaches that a key hashes, all of them but unsafe's and
// those of a package with no files, and of those the files of the packages in module, the ones no standard-library
// key reads.
func filesKeyed(loaded []*packages.Package, module string) (int, int) {
	files := map[string]bool{}
	moduleFiles := map[string]bool{}
	packages.Visit(loaded, nil, func(visited *packages.Package) {
		if visited.PkgPath == "unsafe" {
			return
		}
		for _, list := range [][]string{visited.GoFiles, visited.CompiledGoFiles, visited.OtherFiles,
			visited.EmbedFiles, visited.IgnoredFiles} {
			for _, file := range list {
				files[file] = true
				if strings.HasPrefix(visited.PkgPath, module) {
					moduleFiles[file] = true
				}
			}
		}
	})
	return len(files), len(moduleFiles)
}

// A run hashes each file once: a cached run hashes every file for its keys and the standard library's keys read
// those digests, and a run with no store hashes only the standard library's files. A session that hashed the
// standard library again for its keys would hash some files twice.
//
// Not parallel: it reads cache.FilesHashed, which every test in the process that keys a package moves.
func TestARunHashesEachFileOnce(test *testing.T) {
	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	caching := testCaching(test, rules)
	caching.StandardLibrary = &StandardLibraryCaching{Context: standardLibraryContext("check tests", goRoot)}
	before := cache.FilesHashed()
	cached := runWithSession(test, directory, caching, rules)
	files, moduleFiles := filesKeyed(cached.loaded.Packages, "example.com/standard")
	if hashed := cache.FilesHashed() - before; hashed != int64(files) {
		test.Errorf("want each of the %d files hashed once in a cached run, got %d hashed", files, hashed)
	}
	before = cache.FilesHashed()
	runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
		Context: standardLibraryContext("check tests", goRoot),
	}}, rules)
	if hashed := cache.FilesHashed() - before; hashed != int64(files-moduleFiles) {
		test.Errorf("want the %d standard-library files hashed once with no store, got %d hashed",
			files-moduleFiles, hashed)
	}
}
