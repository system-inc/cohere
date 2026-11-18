package check

import (
	"bytes"
	"encoding/gob"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
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
		FormatVersion:  CacheFormatVersion,
		EngineIdentity: stamp,
		Build:          []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH},
		Goroot:         goRoot,
	}
}

// tableKeyOf is the key a session running rules reads the system cache's table of context by, its fact analyzers
// named as the session names them.
func tableKeyOf(test *testing.T, context cache.StandardLibraryContext, rules []rule.Rule) cache.Key {
	test.Helper()
	session := NewCacheSession(&Caching{}, rules)
	if session.known == nil {
		test.Fatalf("the session knows no analyzers: %v", session.counts.Problems)
	}
	context.Facts = factAnalyzerSet(session.known)
	return cache.StandardLibraryTableKey(context)
}

// systemCacheHolding is a system cache's store of tables of the test's own holding table under key.
func systemCacheHolding(test *testing.T, table []byte, key cache.Key) *cache.Store {
	test.Helper()
	store := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	if putError := store.Put(key, table); putError != nil {
		test.Fatalf("putting the table in the system cache: %v", putError)
	}
	return store
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

// A cached run collects every standard-library package it saves, and the table written from them, read from the
// system cache, restores each one in a run with no store at all: nothing analyzes a package restored from the table,
// nothing parses one, the store counts stay at 0 and the result is a run's without the cache. strings' types come from
// its export data, renamed to this machine's `GOROOT`, so a position of strings.ToUpper names the local file; a table
// read without the rename names `$GOROOT/src/strings`.
func TestAStandardLibraryPackageFromTheSystemCacheIsNeverAnalyzedOrParsed(test *testing.T) {
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
	cold := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(cold)
	if len(collected) == 0 || len(collected) >= cold.result.Cache.Written {
		test.Fatalf("want every standard-library package written collected, and the module not, got %d of %+v",
			len(collected), cold.result.Cache)
	}
	table, _, tableError := StandardLibraryTableOf(collected, goRoot, nil)
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
	context := standardLibraryContext("check tests", goRoot)
	fromSystemCache := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
		System:  systemCacheHolding(test, table, tableKeyOf(test, context, rules)),
		Context: context,
	}}, rules)
	if counted := standardRuns(runs); counted != 0 {
		test.Errorf("want nothing analyzed on a standard-library package from the system cache, got %d runs", counted)
	}
	if want := (CacheCounts{FromSystemCache: len(collected)}); !reflect.DeepEqual(fromSystemCache.result.Cache,
		want) {
		test.Errorf("want every collected package from the system cache and no store count, got %+v",
			fromSystemCache.result.Cache)
	}
	if fromSystemCache.loaded.FilesParsed != 1 {
		test.Errorf("want the module's one file parsed alone, got %d", fromSystemCache.loaded.FilesParsed)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(fromSystemCache.result), withoutTimesOrCache(plain)) {
		test.Errorf("from the system cache\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(fromSystemCache.result),
			withoutTimesOrCache(plain))
	}
	var restoredStrings *packages.Package
	packages.Visit(fromSystemCache.loaded.Packages, nil, func(visited *packages.Package) {
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

// A table written under one engine identity restores nothing under another, nor under another build, nor in a run
// whose fact analyzers' options are not the table's: each table's key differs, and so does each package's, so every
// standard-library package computes.
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
	collectingRun := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(collectingRun)
	table, _, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	system := systemCacheHolding(test, table, tableKeyOf(test, standardLibraryContext("release one", goRoot), rules))
	readUnder := func(context cache.StandardLibraryContext) (CacheCounts, int) {
		test.Helper()
		runs.Clear()
		read := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
			System:  system,
			Context: context,
		}}, rules)
		return read.result.Cache, standardRuns(runs)
	}
	if counts, analyzed := readUnder(standardLibraryContext("release one", goRoot)); counts.FromSystemCache == 0 ||
		analyzed != 0 {
		test.Fatalf("want the table read under its own key, got %+v and %d runs", counts, analyzed)
	}
	otherBuild := standardLibraryContext("release one", goRoot)
	otherBuild.Build = []string{"GOOS=plan9", "GOARCH=" + runtime.GOARCH}
	for name, context := range map[string]cache.StandardLibraryContext{
		"another identity": standardLibraryContext("release two", goRoot),
		"another build":    otherBuild,
	} {
		if counts, analyzed := readUnder(context); counts.FromSystemCache != 0 || analyzed == 0 {
			test.Errorf("%s: want nothing from the system cache and the standard library analyzed, got %+v and %d runs",
				name, counts, analyzed)
		}
	}
	// Last, since the option stays set on the rule's analyzer.
	if setError := rules[0].Analyzer.Flags.Set("mode", "second"); setError != nil {
		test.Fatal(setError)
	}
	if counts, analyzed := readUnder(standardLibraryContext("release one", goRoot)); counts.FromSystemCache != 0 ||
		analyzed == 0 {
		test.Errorf("another fact option: want nothing from the system cache and the standard library analyzed, "+
			"got %+v and %d runs", counts, analyzed)
	}
}

// A session reads the system cache's table only under its own context's key: a table whose entries the session would
// restore, put under the key of another engine identity, another target, another Go version or other fact analyzers,
// restores nothing, as a linux session must never read a darwin table, and the same table under the session's own key
// restores every package. A key that left out the target or the identity would find the table, and the entries in it
// would restore.
func TestATableIsReadOnlyUnderItsOwnContextsKey(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	own := standardLibraryContext("check tests", goRoot)
	own.Build = append(own.Build, "GOVERSION=go1.27.3")
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{Context: own, Collected: collected}
	collectingRun := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(collectingRun)
	table, _, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	otherSystem := "linux"
	if runtime.GOOS == "linux" {
		otherSystem = "darwin"
	}
	otherTarget := own
	otherTarget.Build = []string{"GOOS=" + otherSystem, "GOARCH=" + runtime.GOARCH, "GOVERSION=go1.27.3"}
	otherVersion := own
	otherVersion.Build = []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH, "GOVERSION=go1.26.0"}
	otherIdentity := own
	otherIdentity.EngineIdentity = "another engine"
	for name, testCase := range map[string]struct {
		key  cache.Key
		want int
	}{
		"its own context":      {key: tableKeyOf(test, own, rules), want: len(collected)},
		"another target":       {key: tableKeyOf(test, otherTarget, rules)},
		"another Go version":   {key: tableKeyOf(test, otherVersion, rules)},
		"another identity":     {key: tableKeyOf(test, otherIdentity, rules)},
		"other fact analyzers": {key: tableKeyOf(test, own, nil)},
	} {
		runs.Clear()
		read := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
			System:  systemCacheHolding(test, table, testCase.key),
			Context: own,
		}}, rules)
		if read.result.Cache.FromSystemCache != testCase.want {
			test.Errorf("a table under the key of %s: want %d packages from the system cache, got %+v", name,
				testCase.want, read.result.Cache)
		}
	}
}

// A table damaged in the system cache, one byte of its file changed, is a problem the session names, and every
// standard-library package is computed: the store reads the damage as a miss and removes the file, and the session,
// which found a file there, says the table was damaged rather than that there was none.
func TestADamagedTableIsANamedProblem(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	context := standardLibraryContext("check tests", goRoot)
	collecting := testCaching(test, rules)
	collected := map[cache.Key]StandardLibraryRunEntry{}
	collecting.StandardLibrary = &StandardLibraryCaching{Context: context, Collected: collected}
	collectingRun := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(collectingRun)
	table, _, tableError := StandardLibraryTableOf(collected, goRoot, nil)
	if tableError != nil {
		test.Fatalf("writing the table: %v", tableError)
	}
	key := tableKeyOf(test, context, rules)
	tables := filepath.Join(test.TempDir(), "go", "standard-library")
	system := cache.OpenAt(tables)
	if putError := system.Put(key, table); putError != nil {
		test.Fatal(putError)
	}
	// The store keeps an entry under a directory named for its key's first two hex characters.
	path := filepath.Join(tables, key.String()[:2], key.String())
	contents, readError := os.ReadFile(path)
	if readError != nil {
		test.Fatal(readError)
	}
	contents[len(contents)/2]++
	if writeError := os.WriteFile(path, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}

	runs.Clear()
	read := runWithSession(test, directory, &Caching{StandardLibrary: &StandardLibraryCaching{
		System:  system,
		Context: context,
	}}, rules)
	if read.result.Cache.FromSystemCache != 0 || standardRuns(runs) == 0 {
		test.Errorf("want the standard library computed beside a damaged table, got %+v and %d runs",
			read.result.Cache, standardRuns(runs))
	}
	if problems := read.result.Cache.Problems; len(problems) != 1 || !strings.Contains(problems[0], "damaged") {
		test.Errorf("want the damaged table named as the one problem, got %q", problems)
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
	written, _, writeError := StandardLibraryTableOf(map[cache.Key]StandardLibraryRunEntry{
		key: {PackagePath: "net", Entry: encoded(portable), AnswersWhole: true, ReadsJudged: true},
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
		if _, _, refusedError := StandardLibraryTableOf(map[cache.Key]StandardLibraryRunEntry{
			key: {PackagePath: "net", Entry: encoded(refused), AnswersWhole: true, ReadsJudged: true},
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

// moduleImporting writes a module of the test's own whose one package imports each of imported, a standard-library
// package, and returns its directory.
func moduleImporting(test *testing.T, imported ...string) string {
	test.Helper()
	directory := test.TempDir()
	source := "package importing\n\nimport (\n"
	for _, path := range imported {
		source += "\t_ \"" + path + "\"\n"
	}
	source += ")\n"
	for name, contents := range map[string]string{
		"go.mod":       "module example.com/importing\n\ngo 1.24\n",
		"importing.go": source,
	} {
		if writeError := os.WriteFile(filepath.Join(directory, name), []byte(contents), 0o644); writeError != nil {
			test.Fatalf("writing %s: %v", name, writeError)
		}
	}
	return directory
}

// systemCacheCaching is a findings cache with no store that reads and writes the system cache's store of tables
// system under context, collecting what it computes, as a run under `--no-cache` keeping a system cache does.
func systemCacheCaching(system *cache.Store, context cache.StandardLibraryContext) *Caching {
	return &Caching{StandardLibrary: &StandardLibraryCaching{
		System:    system,
		Context:   context,
		Collected: map[cache.Key]StandardLibraryRunEntry{},
	}}
}

// runSessionWith is runWithSession with the session opened for sessionRules and the run made of runRules, so a test
// can have the session expect an analyzer the run never runs, and with what each package read of others recorded, as
// a run keeping a system cache records it, so the entries it collects persist their reads (PersistCollectedReads).
func runSessionWith(
	test *testing.T,
	directory string,
	caching *Caching,
	sessionRules []rule.Rule,
	runRules []rule.Rule,
) sessionRun {
	test.Helper()
	session := NewCacheSession(caching, sessionRules)
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session, RecordReads: load.EveryPackage})
	if loadError != nil {
		test.Fatalf("loading %s: %v", directory, loadError)
	}
	result, runError := Run(loaded.Packages, runRules, Options{Cache: session, Withheld: loaded.Withheld,
		Reads: loaded.Reads})
	if runError != nil {
		test.Fatalf("running %d rules: %v", len(runRules), runError)
	}
	return sessionRun{loaded: loaded, result: result, session: session}
}

// persistRunReads has run's session persist its collected entries' reads once every type-read walk has ended, as a
// finished run does (PersistCollectedReads).
func persistRunReads(run sessionRun) {
	run.result.ReadWalks.Wait()
	run.session.PersistCollectedReads(run.result.Reads)
}

// writtenTable has run's session write its table back (WriteSystemCacheTable), its reads judged first as a finished
// run's are, and returns how many entries it added.
func writtenTable(test *testing.T, run sessionRun) int {
	test.Helper()
	persistRunReads(run)
	added, writeError := run.session.WriteSystemCacheTable()
	if writeError != nil {
		test.Fatalf("writing the system cache's table: %v", writeError)
	}
	return added
}

// standardKeyOf is the standard-library key run's session gave the package at path.
func standardKeyOf(test *testing.T, run sessionRun, path string) cache.Key {
	test.Helper()
	for keyed, key := range run.session.standardLibraryKeys {
		if keyed.PkgPath == path {
			return key
		}
	}
	test.Fatalf("the session keyed no standard-library package %s", path)
	return cache.Key{}
}

// A short entry never reaches the system cache's table (standardLibraryEntryWhole, #e6wnn8r), as a table hit is taken
// as whole and an entry the table holds is never written again. A package checked where no fact analyzer's action
// reached it, here every standard-library package of a run whose session expects a fact analyzer the run never runs,
// is left out, and a table of nothing is never written. A package whose answers were computed in part (AnswersInPart)
// is left out of a table that takes every whole entry beside it, and so is each package that imports it, whose export
// data could not be read without it.
func TestAShortEntryNeverReachesTheTable(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	context := standardLibraryContext("check tests", goRoot)
	factless := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	counting := countingFactRule(&sync.Map{})
	clauses := houseRule(packageClauseAnalyzer)
	run := runSessionWith(test, directory, systemCacheCaching(factless, context), []rule.Rule{counting, clauses},
		[]rule.Rule{clauses})
	if added := writtenTable(test, run); added != 0 {
		test.Errorf("want no entry checked without its fact analyzer in the table, got %d added", added)
	}
	if factless.Holds(tableKeyOf(test, context, []rule.Rule{counting, clauses})) {
		test.Error("want no table written of entries that are all short")
	}

	inPart := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	rules := []rule.Rule{counting, houseRule(ctrlflow.Analyzer)}
	caching := systemCacheCaching(inPart, context)
	caching.AnswersInPart = map[string]bool{"strings": true}
	partial := runSessionWith(test, directory, caching, rules, rules)
	if added := writtenTable(test, partial); added == 0 {
		test.Fatal("want the whole entries beside strings' in the table")
	}
	contents, found := inPart.Get(tableKeyOf(test, context, rules))
	if !found {
		test.Fatal("want the table written")
	}
	table := cache.NewStandardLibraryTable(contents)
	if _, held := table.Lookup(standardKeyOf(test, partial, "strings")); held {
		test.Error("want strings, its answers computed in part, left out of the table")
	}
	if _, held := table.Lookup(standardKeyOf(test, partial, "unicode/utf8")); !held {
		test.Error("want unicode/utf8, whole and importing nothing short, in the table")
	}
}

// Two sessions that read the same table and each add entries of their own write in turn, and the later rename wins:
// the table reads whole, and a run over either module restores what the table holds and computes the rest, its
// result the one of a run without the cache, so every key hits or misses and never reads wrong.
func TestTwoWritersOfOneTableNeverReadWrong(test *testing.T) {
	test.Parallel()

	goRoot := goRootOf(test)
	context := standardLibraryContext("check tests", goRoot)
	system := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	first, second := moduleImporting(test, "errors"), moduleImporting(test, "unicode/utf8")
	// Both read the table before either writes, as two runs at once do.
	firstRun := runSessionWith(test, first, systemCacheCaching(system, context), rules, rules)
	secondRun := runSessionWith(test, second, systemCacheCaching(system, context), rules, rules)
	if writtenTable(test, firstRun) == 0 || writtenTable(test, secondRun) == 0 {
		test.Fatal("want each writer to add entries of its own")
	}
	contents, found := system.Get(tableKeyOf(test, context, rules))
	if !found {
		test.Fatal("want the table written")
	}
	if problem := cache.NewStandardLibraryTable(contents).Problem(); problem != nil {
		test.Fatalf("want the table to read whole, got %v", problem)
	}
	for _, directory := range []string{first, second} {
		plainLoad, loadError := load.Packages(directory, []string{"./..."}, []string{"GOWORK=off"}, nil)
		if loadError != nil {
			test.Fatal(loadError)
		}
		plain, plainError := Run(plainLoad.Packages, rules, Options{})
		if plainError != nil {
			test.Fatal(plainError)
		}
		// Recording no reads, as the run without the cache records none, so the results compare whole.
		read := runWithSession(test, directory, systemCacheCaching(system, context), rules)
		if !reflect.DeepEqual(withoutTimesOrCache(read.result), withoutTimesOrCache(plain)) {
			test.Errorf("%s: over the table two writers wrote\n%+v\nwithout the cache\n%+v", directory,
				withoutTimesOrCache(read.result), withoutTimesOrCache(plain))
		}
	}
	reread := runSessionWith(test, second, systemCacheCaching(system, context), rules, rules)
	if reread.result.Cache.FromSystemCache != len(reread.session.standardLibraryKeys) {
		test.Errorf("want the later writer's every package restored, got %d of %d", reread.result.Cache.FromSystemCache,
			len(reread.session.standardLibraryKeys))
	}
}

// seededTable is the table of every standard-library package a run over the standard library module collects under
// context with rules, by key, as change leaves it, put in a system cache of the test's own under the key a session
// with rules reads it by, and the collecting run, whose session names each package's key (standardKeyOf).
func seededTable(
	test *testing.T,
	directory string,
	context cache.StandardLibraryContext,
	rules []rule.Rule,
	change func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry),
) (*cache.Store, sessionRun) {
	test.Helper()
	collecting := systemCacheCaching(nil, context)
	collectingRun := runSessionWith(test, directory, collecting, rules, rules)
	persistRunReads(collectingRun)
	written, _, writeError := StandardLibraryTableOf(collecting.StandardLibrary.Collected, context.Goroot, nil)
	if writeError != nil {
		test.Fatal(writeError)
	}
	whole := cache.NewStandardLibraryTable(written)
	entries := map[cache.Key]cache.StandardLibraryEntry{}
	for _, key := range whole.Keys() {
		// Ignored: the key is one of the table's own.
		entries[key], _ = whole.Lookup(key)
	}
	change(collectingRun, entries)
	return systemCacheHolding(test, cache.WriteStandardLibraryTable(entries, nil), tableKeyOf(test, context, rules)),
		collectingRun
}

// A good entry the table already holds under a key is kept as it is, never written again, so two runs never churn one
// key: a table whose entry of unicode/utf8 lacks the analysis of the run's fact analyzer, which dropShortHits drops so
// the run computes utf8 again, and which lacks strings, so the run adds an entry and writes, still holds utf8's entry
// byte for byte after the write. It decodes and its export data reads, so it is no damage to replace.
func TestAGoodEntryTheTableHoldsIsNeverRewritten(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	var planted []byte
	var utf8Key cache.Key
	var stringsKey cache.Key
	system, _ := seededTable(test, directory, context, rules,
		func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry) {
			utf8Key, stringsKey = standardKeyOf(test, collecting, "unicode/utf8"), standardKeyOf(test, collecting,
				"strings")
			delete(entries, stringsKey)
			held := entries[utf8Key]
			tableEntry, decodeError := decodeStandardLibraryEntry(held.Entry)
			if decodeError != nil {
				test.Fatal(decodeError)
			}
			tableEntry.Analyzers = nil
			short, encodeError := encodeStandardLibraryEntry(tableEntry)
			if encodeError != nil {
				test.Fatal(encodeError)
			}
			held.Entry, planted = short, short
			entries[utf8Key] = held
		})

	run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
	if added := writtenTable(test, run); added == 0 {
		test.Fatal("want the run to add strings, which the table lacked")
	}
	contents, found := system.Get(tableKeyOf(test, context, rules))
	if !found {
		test.Fatal("want the table written")
	}
	table := cache.NewStandardLibraryTable(contents)
	if entry, held := table.Lookup(utf8Key); !held || !bytes.Equal(entry.Entry, planted) {
		test.Errorf("want utf8's good entry kept as the table held it (held %v)", held)
	}
	if _, held := table.Lookup(stringsKey); !held {
		test.Error("want strings added")
	}
}

// A held entry that is damage, its export data unreadable, is replaced by the run's whole entry under its key, never
// kept: the run computes unicode/utf8 again, since its held entry does not restore, and strings, which the table
// lacked, read against the run's utf8, and the next run restores every standard-library package, strings with it,
// from what the write left. Kept, the damaged utf8 would fail every reader's restore of utf8 and of strings, for good.
func TestADamagedHeldEntryIsReplaced(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	system, _ := seededTable(test, directory, context, rules,
		func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry) {
			delete(entries, standardKeyOf(test, collecting, "strings"))
			utf8Key := standardKeyOf(test, collecting, "unicode/utf8")
			damaged := entries[utf8Key]
			damaged.ExportData = []byte("not export data")
			entries[utf8Key] = damaged
		})
	run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
	if added := writtenTable(test, run); added == 0 {
		test.Fatal("want the run to write its table")
	}
	next := runWithSession(test, directory, systemCacheCaching(system, context), rules)
	if next.result.Cache.FromSystemCache != len(next.session.standardLibraryKeys) {
		test.Errorf("want every one of the %d standard-library packages restored after the write, got %d",
			len(next.session.standardLibraryKeys), next.result.Cache.FromSystemCache)
	}
}

// An entry the table holds that decodes and whose export data reads, but that does not restore, is said as a problem,
// computed again, and replaced at the run's write by the judgment that refused it (refusedFromTable): a table whose
// unicode/utf8 names a withheld error in a file outside `$GOROOT`, which no restore can resolve, has the run say so,
// and the next run restores every standard-library package from the table its write left.
func TestAnEntryThatDoesNotRestoreIsSaidAndReplaced(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	system, _ := seededTable(test, directory, context, rules,
		func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry) {
			delete(entries, standardKeyOf(test, collecting, "strings"))
			utf8Key := standardKeyOf(test, collecting, "unicode/utf8")
			unresolvable := entries[utf8Key]
			tableEntry, decodeError := decodeStandardLibraryEntry(unresolvable.Entry)
			if decodeError != nil {
				test.Fatal(decodeError)
			}
			tableEntry.Withheld.Errors = append(tableEntry.Withheld.Errors,
				load.WithheldError{File: "/elsewhere/utf8.go", Line: 1, Column: 1, Message: "withheld"})
			encoded, encodeError := encodeStandardLibraryEntry(tableEntry)
			if encodeError != nil {
				test.Fatal(encodeError)
			}
			unresolvable.Entry = encoded
			entries[utf8Key] = unresolvable
		})
	run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
	if !slices.ContainsFunc(run.result.Cache.Problems, func(problem string) bool {
		return strings.Contains(problem, "restoring unicode/utf8 from the system cache's table")
	}) {
		test.Errorf("want utf8's refused restore said as a problem, got %q", run.result.Cache.Problems)
	}
	if added := writtenTable(test, run); added == 0 {
		test.Fatal("want the run to write its table")
	}
	next := runWithSession(test, directory, systemCacheCaching(system, context), rules)
	if next.result.Cache.FromSystemCache != len(next.session.standardLibraryKeys) {
		test.Errorf("want every one of the %d standard-library packages restored after the write, got %d",
			len(next.session.standardLibraryKeys), next.result.Cache.FromSystemCache)
	}
}

// A run without a project's store, as under `--no-cache`, names the owner of every answer a standard-library package
// read by its standard-library key, so none of its entries is collected with reads it could not name: taken by the
// system cache's table so, such an entry would have every extra build decline it, and what imports it, at the check,
// and load again, for as long as the table holds it. Over the engine's standard-library fixture, with every rule, 36
// of its 78 entries read answers of other packages.
func TestARunWithoutAStoreNamesEveryStandardLibraryRead(test *testing.T) {
	test.Parallel()

	directory := test.TempDir()
	for _, name := range []string{"go.mod", "library.go"} {
		contents, readError := os.ReadFile(filepath.Join("..", "testdata", "standard-library", name))
		if readError != nil {
			test.Fatal(readError)
		}
		if writeError := os.WriteFile(filepath.Join(directory, name), contents, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	every := rules.All()
	caching := systemCacheCaching(nil, standardLibraryContext("check tests", goRootOf(test)))
	persistRunReads(runSessionWith(test, directory, caching, every, every))
	withReads := 0
	for key, collected := range caching.StandardLibrary.Collected {
		var entry cacheEntry
		if decodeError := gob.NewDecoder(bytes.NewReader(collected.Entry)).Decode(&entry); decodeError != nil {
			test.Fatal(decodeError)
		}
		if entry.ReadsIncomplete {
			test.Errorf("%s, under %s, was collected with answer reads it could not name", collected.PackagePath, key)
		}
		if len(entry.Reads) > 0 {
			withReads++
		}
	}
	if withReads == 0 {
		test.Errorf("no entry of the %d collected read an answer of another package, so the run tells nothing",
			len(caching.StandardLibrary.Collected))
	}
}

// A run whose table holds every standard-library package it computes collects nothing, so it persists no reads and
// merges no table (tableHoldsNot): a table whose unicode/utf8 lacks the analysis of the run's fact analyzer, which
// dropShortHits drops so the run computes utf8 again, holds utf8 still, so the run's utf8 is never collected, and with
// nothing else new the write adds nothing.
func TestARunTheTableHoldsAllItComputesCollectsNothing(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	system, _ := seededTable(test, directory, context, rules,
		func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry) {
			utf8Key := standardKeyOf(test, collecting, "unicode/utf8")
			held := entries[utf8Key]
			tableEntry, decodeError := decodeStandardLibraryEntry(held.Entry)
			if decodeError != nil {
				test.Fatal(decodeError)
			}
			tableEntry.Analyzers = nil
			short, encodeError := encodeStandardLibraryEntry(tableEntry)
			if encodeError != nil {
				test.Fatal(encodeError)
			}
			held.Entry = short
			entries[utf8Key] = held
		})
	caching := systemCacheCaching(system, context)
	run := runSessionWith(test, directory, caching, rules, rules)
	if run.result.Cache.FromSystemCache == len(run.session.standardLibraryKeys) {
		test.Fatal("the run restored every package from the table, so it computed utf8 nowhere and tells nothing")
	}
	if collected := caching.StandardLibrary.Collected; len(collected) != 0 {
		paths := make([]string, 0, len(collected))
		for _, entry := range collected {
			paths = append(paths, entry.PackagePath)
		}
		test.Errorf("want nothing collected of what the table holds, got %q", paths)
	}
	if added := writtenTable(test, run); added != 0 {
		test.Errorf("want the write to add nothing, got %d", added)
	}
}

// An entry the table would refuse is never collected (wholeForTable), nor encoded where nothing else takes it, so a run
// never persists its reads and merges the table only for the table to refuse it again: a run whose session expects a
// fact analyzer it never runs collects no standard-library package, and one whose strings answers were computed in
// part collects no strings.
func TestAnEntryTheTableWouldRefuseIsNeverCollected(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	counting := countingFactRule(&sync.Map{})
	clauses := houseRule(packageClauseAnalyzer)
	factless := systemCacheCaching(nil, context)
	runSessionWith(test, directory, factless, []rule.Rule{counting, clauses}, []rule.Rule{clauses})
	if collected := len(factless.StandardLibrary.Collected); collected != 0 {
		test.Errorf("want no entry checked without its fact analyzer collected, got %d", collected)
	}
	// Judged before its answers are computed whole: a run that answers on demand, with ctrlflow, but never runs the
	// counting fact analyzer its session expects, computes no package's answers.
	answering := houseRule(ctrlflow.Analyzer)
	unanswered := runSessionWith(test, directory, systemCacheCaching(nil, context), []rule.Rule{counting, answering},
		[]rule.Rule{answering})
	if whole := unanswered.session.answersComputedWhole.Load(); whole != 0 {
		test.Errorf("want no answers computed whole for entries short of a fact analyzer, got %d", whole)
	}

	rules := []rule.Rule{counting, houseRule(ctrlflow.Analyzer)}
	inPart := systemCacheCaching(nil, context)
	inPart.AnswersInPart = map[string]bool{"strings": true}
	partial := runSessionWith(test, directory, inPart, rules, rules)
	if _, collected := inPart.StandardLibrary.Collected[standardKeyOf(test, partial, "strings")]; collected {
		test.Error("want strings, its answers computed in part, never collected")
	}
	if _, collected := inPart.StandardLibrary.Collected[standardKeyOf(test, partial, "unicode/utf8")]; !collected {
		test.Error("want unicode/utf8, whole, collected")
	}
}

// A run that records reads for the system cache alone records none of a package its table restored
// (RecordsReadsForTable): over a table that holds every standard-library package of the module, the load records the
// reads of nothing, since the module's own package is no table's.
func TestAPackageTheTableRestoredRecordsNoReads(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	system, _ := seededTable(test, directory, context, rules,
		func(sessionRun, map[cache.Key]cache.StandardLibraryEntry) {
			// The table as the run wrote it, whole.
		})
	session := NewCacheSession(systemCacheCaching(system, context), rules)
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, []string{"GOWORK=off"}, nil,
		load.Caching{Restorer: session, Listings: session, RecordReads: session.RecordsReadsForTable})
	if loadError != nil {
		test.Fatal(loadError)
	}
	if len(session.fromSystemCache) != len(session.standardLibraryKeys) {
		test.Fatalf("want every one of the %d standard-library packages restored from the table, got %d",
			len(session.standardLibraryKeys), len(session.fromSystemCache))
	}
	if loaded.ReadsRecorded != 0 {
		test.Errorf("want no package's reads recorded, got %d", loaded.ReadsRecorded)
	}
}

// storelessSystemCacheRun is a session without a store keeping a system cache at system, as under `--no-cache`, run
// over directory under environment with rules, the host's run of a run with extra builds when host is nil, kept for
// them, or a build restoring from host, each recording the reads its system cache records.
func storelessSystemCacheRun(
	test *testing.T,
	directory string,
	system *cache.Store,
	rules []rule.Rule,
	environment []string,
	host *HostRun,
) (*CacheSession, *Result) {
	test.Helper()
	context := standardLibraryContext("check tests", goRootOf(test))
	context.Build = append(context.Build, environment...)
	caching := systemCacheCaching(system, context)
	caching.Context.Build = environment
	session := NewCacheSession(caching, rules)
	recordReads := session.RecordsReadsForTable
	if host == nil {
		session.KeepForBuilds()
		recordReads = load.EveryPackage
	} else {
		session.RestoreFromHost(host)
	}
	loaded, loadError := load.PackagesCached(directory, []string{"./..."}, append([]string{"GOWORK=off"},
		environment...), nil, load.Caching{Restorer: session, RecordReads: recordReads})
	if loadError != nil || len(loaded.Diagnostics) > 0 {
		test.Fatalf("loading under %q: %v %+v", environment, loadError, loaded.Diagnostics)
	}
	result, runError := Run(loaded.Packages, rules, Options{Cache: session, Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots, KeepForBuilds: host == nil})
	if runError != nil || len(result.Crashes) > 0 {
		test.Fatalf("running under %q: %v %+v", environment, runError, result.Crashes)
	}
	result.ReadWalks.Wait()
	return session, result
}

// A warm extra build without a store never computes the answers of a standard-library package whole only for its
// table's write to leave it out (importsReadableAtWrite): a windows build restoring its standard library from the
// host's run checks from source the packages windows compiles otherwise, above packages restored from the host, which
// its own table never holds, so the write could read none of their export data. Its cold run writes the few it can
// read, those importing only what it computed too, and its warm run restores those and computes no package's answers
// whole, where judging them after the answers, as the profile of #syscache's pair found, computes them for each of the
// others on every run.
func TestAWarmBuildComputesNoAnswersForAnEntryItsTableCouldNotRead(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{}), houseRule(ctrlflow.Analyzer)}
	system := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	_, hostResult := storelessSystemCacheRun(test, directory, system, rules, nil, nil)
	if hostResult.Host.EntriesKept() == 0 {
		test.Fatal("the host's run kept no entry, so the build restores nothing from it")
	}
	cold, coldResult := storelessSystemCacheRun(test, directory, system, rules, []string{"GOOS=windows"},
		hostResult.Host)
	persistRunReads(sessionRun{result: coldResult, session: cold})
	if added, writeError := cold.WriteSystemCacheTable(); writeError != nil || added == 0 {
		test.Fatalf("want the cold windows build to write its table, added %d: %v", added, writeError)
	}
	build, _ := storelessSystemCacheRun(test, directory, system, rules, []string{"GOOS=windows"}, hostResult.Host)
	if len(build.fromSystemCache) == 0 {
		test.Error("want the warm windows build to restore from its own table")
	}
	computed := 0
	for keyed := range build.standardLibraryKeys {
		if !build.restored(keyed) {
			computed++
		}
	}
	if computed == 0 {
		test.Fatal("the windows build restored every standard-library package, so it tells nothing")
	}
	if whole := build.answersComputedWhole.Load(); whole != 0 {
		test.Errorf("the windows build computed the answers of %d packages whole, of the %d it checked, want none",
			whole, computed)
	}
}

// A read takes, of what the table held, only the entries under the reading run's own keys: a table holding a second
// entry of unicode/utf8's path, under the key of a Go installation since changed, beside the run's own, still has the
// run's new strings read against the run's utf8 and taken. Read against every held entry, utf8's path would be two
// packages, and strings, which imports it, would be left out.
func TestAReadTakesOnlyTheRunsOwnHeldEntries(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	var stringsKey cache.Key
	system, _ := seededTable(test, directory, context, rules,
		func(collecting sessionRun, entries map[cache.Key]cache.StandardLibraryEntry) {
			stringsKey = standardKeyOf(test, collecting, "strings")
			delete(entries, stringsKey)
			entries[cache.DigestOf([]byte("utf8 of another installation"))] =
				entries[standardKeyOf(test, collecting, "unicode/utf8")]
		})
	run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
	if added := writtenTable(test, run); added == 0 {
		test.Fatal("want the run to add strings")
	}
	contents, found := system.Get(tableKeyOf(test, context, rules))
	if !found {
		test.Fatal("want the table written")
	}
	if _, held := cache.NewStandardLibraryTable(contents).Lookup(stringsKey); !held {
		test.Error("want strings, read against the run's own utf8, in the table")
	}
}

// A table that cannot be written keeps nothing from the store: with a regular file where the system cache's
// directory goes, the run's write fails, and every standard-library entry the run computed, whole or short, is in the
// project's store, where the next run hits it.
func TestAStandardLibraryEntryATableCouldNotTakeIsStored(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	file := filepath.Join(test.TempDir(), "system-cache")
	if writeError := os.WriteFile(file, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	caching := testCaching(test, rules)
	caching.StandardLibrary = systemCacheCaching(cache.OpenAt(filepath.Join(file, "go", "standard-library")),
		context).StandardLibrary
	run := runSessionWith(test, directory, caching, rules, rules)
	persistRunReads(run)
	if _, writeError := run.session.WriteSystemCacheTable(); writeError == nil {
		test.Fatal("want the write under a file to fail")
	}
	for keyed := range run.session.standardLibraryKeys {
		if !caching.Store.Holds(run.session.keys[keyed]) {
			test.Errorf("%s is in neither the table nor the project's store", keyed.PkgPath)
		}
	}
}

// standardLibraryEntryWhole reads each of its three things on its own: an entry whole in each is taken, and one short
// in any one of them is refused, a fact analyzer's analysis missing, its answers computed in part or a kind of them
// not complete, or its reads never judged.
func TestTheWholeCheckReadsEachOfItsThings(test *testing.T) {
	test.Parallel()

	whole := StandardLibraryRunEntry{FactAnalyzers: []string{"facts"}, AnswersWhole: true, ReadsJudged: true}
	entry := cacheEntry{
		Analyzers: map[string]storedAnalysis{"facts": {}},
		Answers:   []kindAnswers{{Kind: "purity", Complete: true}},
	}
	if !standardLibraryEntryWhole(whole, entry) {
		test.Fatal("want a whole entry taken")
	}
	missingFacts := whole
	missingFacts.FactAnalyzers = []string{"facts", "other"}
	answersInPart := whole
	answersInPart.AnswersWhole = false
	readsUnjudged := whole
	readsUnjudged.ReadsJudged = false
	kindInPart := entry
	kindInPart.Answers = []kindAnswers{{Kind: "purity", Complete: false}}
	for name, short := range map[string]struct {
		collected StandardLibraryRunEntry
		entry     cacheEntry
	}{
		"a fact analyzer's analysis missing": {missingFacts, entry},
		"answers computed in part":           {answersInPart, entry},
		"a kind of answers not complete":     {whole, kindInPart},
		"reads never judged":                 {readsUnjudged, entry},
	} {
		if standardLibraryEntryWhole(short.collected, short.entry) {
			test.Errorf("%s: want the entry refused", name)
		}
	}
}

// The project's store takes no whole standard-library entry the system cache's table holds, and takes every one the
// table refuses (WriteSystemCacheTable): after a cached run whose answers of strings are computed in part, which leaves
// strings short and out of the table, the store holds the module's package and strings under their keys, and none of
// the packages the table holds under theirs.
func TestTheProjectStoreTakesNoWholeEntryTheTableHolds(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	context := standardLibraryContext("check tests", goRoot)
	rules := []rule.Rule{countingFactRule(&sync.Map{}), houseRule(ctrlflow.Analyzer)}
	system := cache.OpenAt(filepath.Join(test.TempDir(), "go", "standard-library"))
	caching := testCaching(test, rules)
	caching.StandardLibrary = systemCacheCaching(system, context).StandardLibrary
	caching.AnswersInPart = map[string]bool{"strings": true}
	run := runSessionWith(test, directory, caching, rules, rules)
	if writtenTable(test, run) == 0 {
		test.Fatal("want the whole entries in the table")
	}
	contents, found := system.Get(tableKeyOf(test, context, rules))
	if !found {
		test.Fatal("want the table written")
	}
	table := cache.NewStandardLibraryTable(contents)
	held := 0
	for keyed, key := range run.session.standardLibraryKeys {
		_, inTable := table.Lookup(key)
		if inTable {
			held++
		}
		if inStore := caching.Store.Holds(run.session.keys[keyed]); inStore == inTable {
			test.Errorf("%s: in the table %t and in the project's store %t, want one or the other", keyed.PkgPath,
				inTable, inStore)
		}
	}
	if held == 0 {
		test.Error("want the table to hold the whole entries")
	}
	if !caching.Store.Holds(sessionKey(test, run.session, "strings")) {
		test.Error("want strings, short, in the project's store")
	}
	if !caching.Store.Holds(sessionKey(test, run.session, "example.com/standard")) {
		test.Error("want the module's own package in the project's store")
	}
}

// Two Go versions keep two tables side by side in one system cache, neither read by the other's run: the version is
// in each table's key, so a run under the second finds no table of its own beside the first's, computes its standard
// library and writes its own, and a later run under each restores every package from its own.
func TestTwoGoVersionsKeepTablesSideBySide(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	tables := filepath.Join(test.TempDir(), "go", "standard-library")
	system := cache.OpenAt(tables)
	contexts := map[string]cache.StandardLibraryContext{}
	for _, version := range []string{"go1.27.3", "go1.26.0"} {
		context := standardLibraryContext("check tests", goRoot)
		context.Build = append(context.Build, "GOVERSION="+version)
		contexts[version] = context
		run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
		if run.result.Cache.FromSystemCache != 0 {
			test.Errorf("%s: want nothing read of the other version's table, got %d restored", version,
				run.result.Cache.FromSystemCache)
		}
		if writtenTable(test, run) == 0 {
			test.Fatalf("%s: want its table written", version)
		}
	}
	files := 0
	walkError := filepath.WalkDir(tables, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError == nil && entry.Type().IsRegular() {
			files++
		}
		return pathError
	})
	if walkError != nil || files != 2 {
		test.Errorf("want two tables side by side, got %d files (%v)", files, walkError)
	}
	for version, context := range contexts {
		run := runSessionWith(test, directory, systemCacheCaching(system, context), rules, rules)
		if run.result.Cache.FromSystemCache != len(run.session.standardLibraryKeys) {
			test.Errorf("%s: want every package restored from its own table, got %d of %d", version,
				run.result.Cache.FromSystemCache, len(run.session.standardLibraryKeys))
		}
	}
}
