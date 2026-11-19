package check

import (
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

// systemCacheCaching is a findings cache with no project store that reads and writes the system cache's store system
// under context, as a run under `--no-cache` keeping a system cache does.
func systemCacheCaching(system *cache.Store, context cache.StandardLibraryContext) *Caching {
	return &Caching{StandardLibrary: &StandardLibraryCaching{System: system, Context: context}}
}

// systemStore is a system cache's store of the test's own.
func systemStore(test *testing.T) *cache.Store {
	test.Helper()
	return cache.OpenAt(filepath.Join(test.TempDir(), "go", "store"), nil)
}

// runSessionWith is runWithSession with the session opened for sessionRules and the run made of runRules, so a test
// can have the session expect an analyzer the run never runs, and with what each package read of others recorded, as
// a run keeping a system cache records it.
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

// A run without a project's store, as under `--no-cache`, names the owner of every answer a standard-library package
// read by its standard-library key, so none of the entries it writes to the system cache's store holds reads it could
// not name: such an entry would have every extra build decline it, and what imports it, at the check, and load again.
// Over the engine's standard-library fixture, with every rule, 36 of its 78 entries read answers of other packages.
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
	system := systemStore(test)
	run := runSessionWith(test, directory, systemCacheCaching(system, standardLibraryContext("check tests",
		goRootOf(test))), every, every)
	withReads := 0
	for keyed, key := range run.session.standardLibraryKeys {
		entry, _, stored := storedEntryAt(system, key)
		if !stored {
			test.Fatalf("the system cache's store holds no entry of %s", keyed.PkgPath)
		}
		if entry.ReadsIncomplete {
			test.Errorf("%s was written with answer reads it could not name", keyed.PkgPath)
		}
		if len(entry.Reads) > 0 {
			withReads++
		}
	}
	if withReads == 0 {
		test.Errorf("no entry of the %d written read an answer of another package, so the run tells nothing",
			len(run.session.standardLibraryKeys))
	}
}

// A standard-library package a cold run wrote to the system cache's store is restored from it by a warm run with no
// project store, as under `--no-cache`, like a hit of the project's store: nothing analyzes it, nothing parses it, the
// project's store counts stay at 0, and the result is a run's without the cache. strings' types come from the export
// data the cold run wrote, with this machine's paths, so a position of strings.ToUpper names the local file.
func TestAStandardLibraryPackageFromTheSystemCacheIsNeverAnalyzedOrParsed(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	system := systemStore(test)
	context := standardLibraryContext("check tests", goRoot)
	cold := runWithSession(test, directory, systemCacheCaching(system, context), rules)
	if cold.result.Cache.SystemCacheWritten == 0 || cold.result.Cache.SystemCacheWritten !=
		len(cold.session.standardLibraryKeys) {
		test.Fatalf("want every standard-library package written to the system cache cold, got %d of %d",
			cold.result.Cache.SystemCacheWritten, len(cold.session.standardLibraryKeys))
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
	warm := runWithSession(test, directory, systemCacheCaching(system, context), rules)
	if counted := standardRuns(runs); counted != 0 {
		test.Errorf("want nothing analyzed on a standard-library package from the system cache, got %d runs", counted)
	}
	if want := (CacheCounts{FromSystemCache: cold.result.Cache.SystemCacheWritten}); !reflect.DeepEqual(
		warm.result.Cache, want) {
		test.Errorf("want every package the cold run wrote from the system cache and no other count, got %+v",
			warm.result.Cache)
	}
	if warm.loaded.FilesParsed != 1 {
		test.Errorf("want the module's one file parsed alone, got %d", warm.loaded.FilesParsed)
	}
	if !reflect.DeepEqual(withoutTimesOrCache(warm.result), withoutTimesOrCache(plain)) {
		test.Errorf("from the system cache\n%+v\nwithout the cache\n%+v", withoutTimesOrCache(warm.result),
			withoutTimesOrCache(plain))
	}
	var restoredStrings *packages.Package
	packages.Visit(warm.loaded.Packages, nil, func(visited *packages.Package) {
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

// A standard-library entry in the system cache's store is read only under its own context: a warm run under another
// engine identity, another target, another Go version, another `GOROOT` or other fact analyzers' options keys every
// package apart, restores nothing and analyzes the standard library, as a linux session must never read a darwin
// entry, while one under the context the store was filled under restores every package. A key that left one out would
// find the entries, and they would restore.
func TestAStandardLibraryEntryIsReadOnlyUnderItsOwnContext(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	goRoot := goRootOf(test)
	runs := &sync.Map{}
	rules := []rule.Rule{countingFactRule(runs)}
	system := systemStore(test)
	own := standardLibraryContext("check tests", goRoot)
	own.Build = append(own.Build, "GOVERSION=go1.27.3")
	cold := runWithSession(test, directory, systemCacheCaching(system, own), rules)
	written := cold.result.Cache.SystemCacheWritten
	if written == 0 {
		test.Fatal("want the cold run to write the standard library to the system cache")
	}
	readUnder := func(context cache.StandardLibraryContext) (int, int) {
		test.Helper()
		runs.Clear()
		read := runWithSession(test, directory, systemCacheCaching(system, context), rules)
		return read.result.Cache.FromSystemCache, standardRuns(runs)
	}
	if restored, analyzed := readUnder(own); restored != written || analyzed != 0 {
		test.Fatalf("its own context: want all %d restored and nothing analyzed, got %d and %d runs", written,
			restored, analyzed)
	}
	otherSystem := map[bool]string{true: "darwin", false: "linux"}[runtime.GOOS == "linux"]
	otherTarget := own
	otherTarget.Build = []string{"GOOS=" + otherSystem, "GOARCH=" + runtime.GOARCH, "GOVERSION=go1.27.3"}
	otherVersion := own
	otherVersion.Build = []string{"GOOS=" + runtime.GOOS, "GOARCH=" + runtime.GOARCH, "GOVERSION=go1.26.0"}
	otherIdentity := own
	otherIdentity.EngineIdentity = "another engine"
	for name, context := range map[string]cache.StandardLibraryContext{
		"another target":     otherTarget,
		"another Go version": otherVersion,
		"another identity":   otherIdentity,
	} {
		if restored, analyzed := readUnder(context); restored != 0 || analyzed == 0 {
			test.Errorf("%s: want nothing from the system cache and the standard library analyzed, got %d and %d runs",
				name, restored, analyzed)
		}
	}
	// Last, since the option stays set on the rule's analyzer.
	if setError := rules[0].Analyzer.Flags.Set("mode", "second"); setError != nil {
		test.Fatal(setError)
	}
	if restored, analyzed := readUnder(own); restored != 0 || analyzed == 0 {
		test.Errorf("another fact option: want nothing from the system cache and the standard library analyzed, "+
			"got %d and %d runs", restored, analyzed)
	}
}

// With the system cache kept, the project's store holds no standard-library entry and the system cache's store holds
// each one, under its standard-library key, while the module's own package stays in the project's store. Under
// `--no-system-cache`, a session keeping no system cache, every standard-library entry is in the project's store under
// its project key, as in a build before the system cache.
func TestTheProjectStoreHoldsNoStandardLibraryEntryWhileTheSystemCacheIsKept(test *testing.T) {
	test.Parallel()

	directory := standardLibraryModule(test)
	context := standardLibraryContext("check tests", goRootOf(test))
	rules := []rule.Rule{countingFactRule(&sync.Map{})}
	system := systemStore(test)
	caching := testCaching(test, rules)
	caching.StandardLibrary = &StandardLibraryCaching{System: system, Context: context}
	run := runWithSession(test, directory, caching, rules)
	if len(run.session.standardLibraryKeys) == 0 {
		test.Fatal("the session keyed no standard-library package")
	}
	for keyed, key := range run.session.standardLibraryKeys {
		if run.session.keys[keyed] != key {
			test.Errorf("%s: want it keyed by its standard-library key", keyed.PkgPath)
		}
		if caching.Store.Holds(key) || !system.Holds(key) {
			test.Errorf("%s: in the project's store %t and in the system cache's %t, want the system cache's alone",
				keyed.PkgPath, caching.Store.Holds(key), system.Holds(key))
		}
	}
	if !caching.Store.Holds(sessionKey(test, run.session, "example.com/standard")) {
		test.Error("want the module's own package in the project's store")
	}

	withoutSystemCache := testCaching(test, rules)
	withoutSystemCache.StandardLibrary = &StandardLibraryCaching{Context: context}
	plain := runWithSession(test, directory, withoutSystemCache, rules)
	for _, path := range []string{"strings", "unicode/utf8", "example.com/standard"} {
		if !withoutSystemCache.Store.Holds(sessionKey(test, plain.session, path)) {
			test.Errorf("without the system cache, want %s in the project's store", path)
		}
	}
}

// optionClauseAnalyzer reports each file's package clause with its own option's value, so its findings change with the
// option alone, as a rule's do whose options a person changes, while it learns no fact. Its analyzer is new for each
// call, so a test that sets the option sets it on no other test's.
func optionClauseAnalyzer() *analysis.Analyzer {
	analyzer := &analysis.Analyzer{Name: "optionClause", Doc: "optionClause reports each package clause by its option"}
	mode := analyzer.Flags.String("mode", "first", "the word each finding says")
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			pass.Reportf(file.Name.Pos(), "%s package %s", *mode, file.Name.Name)
		}
		return nil, nil
	}
	return analyzer
}

// A standard-library package Analyze was given, as in a run over the standard library's own tree, is never kept in
// the system cache's store, whose standard-library key holds no rule set: its entry holds its rules' findings, so it
// keeps its project key in the project's store, which holds its rules and their options. Over unicode/utf8 in the
// installation's own source, a run writes no package it was given to the system store, and a run after the rule's
// option changed reports the new option's findings, where a given package kept in the system store would restore the
// old ones.
func TestAGivenStandardLibraryPackageIsNeverKeptInTheSystemCache(test *testing.T) {
	test.Parallel()

	goRoot := goRootOf(test)
	directory := filepath.Join(goRoot, "src", "unicode", "utf8")
	analyzer := optionClauseAnalyzer()
	rules := []rule.Rule{houseRule(analyzer)}
	system := systemStore(test)
	store := test.TempDir()
	context := standardLibraryContext("check tests", goRoot)
	cachingFor := func(mode string) *Caching {
		caching := cachingAt(store, rules)
		caching.Context.RuleSet = []string{rules[0].Name + ` {"mode":"` + mode + `"}`}
		caching.StandardLibrary = &StandardLibraryCaching{System: system, Context: context}
		return caching
	}
	first := runWithSession(test, directory, cachingFor("first"), rules)
	given := 0
	for keyed, key := range first.session.standardLibraryKeys {
		if !first.session.given[keyed] {
			continue
		}
		given++
		if system.Holds(key) || first.session.keys[keyed] == key {
			test.Errorf("%s, given, is keyed by its standard-library key or held by the system cache's store",
				keyed.ID)
		}
	}
	if given == 0 {
		test.Fatal("the session was given no standard-library package, so the run tells nothing")
	}
	if setError := analyzer.Flags.Set("mode", "second"); setError != nil {
		test.Fatal(setError)
	}
	second := runWithSession(test, directory, cachingFor("second"), rules)
	for _, finding := range second.result.Findings {
		if !strings.HasPrefix(finding.Message, "second ") {
			test.Errorf("after the option changed, %s says %q, the old option's finding", finding.File,
				finding.Message)
		}
	}
	if len(second.result.Findings) == 0 {
		test.Error("want the given package's findings")
	}
}
