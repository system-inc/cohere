package go_engine

import (
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/system_cache_testing"
)

// Every run in this package's tests reads a system cache of the test binary's own, never the person's: TestMain set
// `COHERE_SYSTEM_CACHE` to an absolute directory outside their user cache directory (#syscache).
func TestTheTestsSystemCacheIsNotTheUsers(test *testing.T) {
	test.Parallel()

	// Ignored: a machine with no user cache directory has none a test could reach.
	usersCache, _ := os.UserCacheDir()
	system_cache_testing.ExpectIsolated(test, usersCache)
}

// testsSystemCache is the system cache TestMain gave this test binary, which every run reads unless a test names
// another.
var testsSystemCache string

// isolatedSource is source with a new, empty system cache of the run's own, unless source names one or the test set
// the variable to one of its own, which the run then reads. Each run of a test that names none computes the standard
// library and writes its table where no other run reads it, so its records are what they are for a cold system cache
// whatever ran before: tests run side by side, and a test that compares two of its own runs, or counts what one
// restored, would otherwise read tables another run wrote. A test of what a warm system cache does names one, shared by
// the runs it compares (sharingSystemCache).
func isolatedSource(test *testing.T, source standardLibrarySource) standardLibrarySource {
	test.Helper()
	if source.systemCacheRoot == "" && os.Getenv(system_cache_testing.Variable) == testsSystemCache {
		source.systemCacheRoot = test.TempDir()
	}
	return source
}

// sharingSystemCache has a pipeline read from and write to the system cache at root, which every run of a test given
// the same root shares, as a person's runs share theirs.
func sharingSystemCache(root string) func(*pipeline) {
	return func(engine *pipeline) {
		sharing := engine.request.standardLibrary
		sharing.systemCacheRoot = root
		engine.request.standardLibrary = sharing
		engine.systemCache = openSystemCache(engine.request)
	}
}

// runIsolated is the engine run over arguments as Main runs it, with registered as the rules and no standard input, and
// a plain run's standard library read from and written to a system cache of the run's own (isolatedSource).
func runIsolated(
	test *testing.T,
	arguments []string,
	standardOutput io.Writer,
	standardError io.Writer,
	registered []rule.Rule,
	memory *engineMemory,
) int {
	test.Helper()
	return runWithStandardLibrary(arguments, standardOutput, standardError, registered, memory,
		isolatedSource(test, standardLibrarySource{}))
}

// isolatePipeline gives engine a system cache of its own (isolatedSource), unless its request names one: the pipeline
// found its own as it was made, from the request it was made with.
//
// Mutates: engine gains the system cache, in its request and its own
func isolatePipeline(test *testing.T, engine *pipeline) {
	test.Helper()
	isolated := isolatedSource(test, engine.request.standardLibrary)
	if isolated.systemCacheRoot != engine.request.standardLibrary.systemCacheRoot {
		engine.request.standardLibrary = isolated
		engine.systemCache = openSystemCache(engine.request)
	}
}

// systemCacheListing is every file below root, a link as itself, by its path with its size and modification time,
// so a run that wrote, rewrote, touched or removed one shows.
func systemCacheListing(test *testing.T, root string) map[string]string {
	test.Helper()
	listing := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		information, informationError := entry.Info()
		if informationError != nil {
			return informationError
		}
		listing[path] = information.ModTime().String() + " " + information.Mode().String() + " " +
			strconv.FormatInt(information.Size(), 10)
		return nil
	})
	if walkError != nil {
		test.Fatalf("listing %s: %v", root, walkError)
	}
	return listing
}

// tableFileOf is the file reading's system cache keeps the table of context in, as the store names it.
func tableFileOf(reading standardLibrarySource, context cache.StandardLibraryContext) string {
	name := cache.StandardLibraryTableKey(context).String()
	return filepath.Join(standardLibraryTablesDirectory(reading.systemCacheRoot), name[:2], name)
}

// A plain run, its standard library the zero source, reads the system cache `$COHERE_SYSTEM_CACHE` names: a table a
// session of the same engine collected, put there under its build context's key, restores every standard-library
// package, cached and under `--no-cache` alike, and the records are those of a run without the system cache.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, which test.Setenv refuses beside parallel tests.
func TestARunRestoresTheStandardLibraryFromTheSystemCacheItNames(test *testing.T) {
	table, collected, context := standardLibraryTableOf(test, rules.All(), "", nil)
	identity, identityError := cache.ExecutableIdentity()
	if identityError != nil {
		test.Fatal(identityError)
	}
	if context.EngineIdentity != identity {
		test.Fatalf("want the table keyed by the engine's identity %s, got %s", identity, context.EngineIdentity)
	}
	systemCache := test.TempDir()
	if putError := cache.OpenAt(standardLibraryTablesDirectory(systemCache)).Put(
		cache.StandardLibraryTableKey(context), table); putError != nil {
		test.Fatal(putError)
	}
	test.Setenv("COHERE_SYSTEM_CACHE", systemCache)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	// Under `--no-cache` first, which makes no store, so the cached run after it is cold.
	for _, extra := range [][]string{{"--no-cache"}, {}} {
		read := runCachedEngine(test, root, extra...)
		if read.counts.FromSystemCache != len(collected) || read.counts.SystemCacheNotKept != "" {
			test.Errorf("%v: want all %d standard-library packages from the system cache it names, got %+v", extra,
				len(collected), read.counts)
		}
		if read.withoutCounts != plain.withoutCounts {
			test.Errorf("%v: from the system cache\n%s\nwithout it\n%s", extra, read.withoutCounts,
				plain.withoutCounts)
		}
	}
}

// `--no-cache` turns off the project's store and nothing else: the run still reads the system cache, restoring every
// standard-library package from its table, and makes no store in the project.
func TestNoCacheStillReadsTheSystemCache(test *testing.T) {
	test.Parallel()

	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	read := runEngineWith(test, root, readingTable(test, table, context), rules.All(), "--no-cache")
	if read.counts.On || read.counts.FromSystemCache != len(collected) {
		test.Errorf("want the store off and all %d standard-library packages from the system cache, got %+v",
			len(collected), read.counts)
	}
	if _, statError := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(statError) {
		test.Errorf("want no store made under --no-cache, got %v", statError)
	}
}

// `--no-system-cache` neither reads nor writes the system cache: a run under it over a system cache holding the
// fixture's table, an hour and more old so a read would mark it used, and due a trim, restores nothing from it, and
// leaves every file of it as it was, the table's time and the absent trim record among them. Its records are those of
// a run reading the table.
func TestNoSystemCacheLeavesTheSystemCacheAsItWas(test *testing.T) {
	test.Parallel()

	table, collected, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	reading := readingTable(test, table, context)
	aged := time.Now().Add(-2 * time.Hour)
	if timesError := os.Chtimes(tableFileOf(reading, context), aged, aged); timesError != nil {
		test.Fatal(timesError)
	}
	before := systemCacheListing(test, reading.systemCacheRoot)
	root := copyFixture(test, standardLibraryFixture)
	off := runEngineWith(test, root, reading, rules.All(), "--no-cache", "--no-system-cache")
	if off.counts.FromSystemCache != 0 || off.counts.SystemCacheNotKept != "" {
		test.Errorf("want nothing from the system cache under --no-system-cache, got %+v", off.counts)
	}
	after := systemCacheListing(test, reading.systemCacheRoot)
	if len(after) != len(before) || !equalEntries(before, after) {
		test.Errorf("want the system cache as it was under --no-system-cache, before %v, after %v", before, after)
	}
	on := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if on.counts.FromSystemCache != len(collected) || on.withoutCounts != off.withoutCounts {
		test.Errorf("want all %d packages from the system cache without the switch, and the same records, got %+v\n"+
			"%s\nunder the switch\n%s", len(collected), on.counts, on.withoutCounts, off.withoutCounts)
	}
}

// A table damaged in the system cache, one byte of its file changed, is a problem the run names on stderr and counts,
// never an error: every standard-library package is computed, the exit is the plain run's, and so is every record but
// the cache's counts.
func TestADamagedSystemCacheTableIsComputedAndNamed(test *testing.T) {
	test.Parallel()

	table, _, context := standardLibraryTableOf(test, rules.All(), engineTestsStamp, nil)
	reading := readingTable(test, table, context)
	file := tableFileOf(reading, context)
	contents, readError := os.ReadFile(file)
	if readError != nil {
		test.Fatal(readError)
	}
	contents[len(contents)/2]++
	if writeError := os.WriteFile(file, contents, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	root := copyFixture(test, standardLibraryFixture)
	plain := runEngineWith(test, root, standardLibrarySource{}, rules.All(), "--no-cache", "--no-system-cache")
	damaged, standardError := runEngineNoting(test, root, reading, rules.All(), nil, "--no-cache")
	if damaged.counts.FromSystemCache != 0 || damaged.counts.Problems != 1 {
		test.Errorf("want nothing from the damaged table and one problem counted, got %+v", damaged.counts)
	}
	if !strings.Contains(standardError, "damaged") {
		test.Errorf("want the damaged table named on stderr, got %q", standardError)
	}
	if damaged.withoutCounts != plain.withoutCounts {
		test.Errorf("beside a damaged table\n%s\nwithout the system cache\n%s", damaged.withoutCounts,
			plain.withoutCounts)
	}
}

// A system cache whose root cannot be a directory, a regular file where it goes, is not kept, and that is never an
// error: the run completes with the plain run's exit and records, the lint record says why in one line, and stderr
// says it once.
func TestAnUnusableSystemCacheRootIsNotKept(test *testing.T) {
	test.Parallel()

	file := filepath.Join(test.TempDir(), "system-cache")
	if writeError := os.WriteFile(file, []byte("a file, not a directory"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	root := copyFixture(test, standardLibraryFixture)
	plain := runEngineWith(test, root, standardLibrarySource{}, rules.All(), "--no-cache", "--no-system-cache")
	unusable, standardError := runEngineNoting(test, root, standardLibrarySource{systemCacheRoot: file}, rules.All(),
		nil, "--no-cache")
	if reason := unusable.counts.SystemCacheNotKept; reason == "" || strings.ContainsAny(reason, "\r\n") {
		test.Errorf("want one line saying why the system cache was not kept, got %q", reason)
	}
	if strings.Count(standardError, "the system cache was not kept") != 1 {
		test.Errorf("want stderr to say once that the system cache was not kept, got %q", standardError)
	}
	if unusable.exitCode != plain.exitCode {
		test.Errorf("over an unusable root the run exited %d, want the plain run's %d", unusable.exitCode,
			plain.exitCode)
	}
	if unusable.withoutCounts != plain.withoutCounts {
		test.Errorf("over an unusable root\n%s\nwithout the system cache\n%s", unusable.withoutCounts,
			plain.withoutCounts)
	}
	if contents, readError := os.ReadFile(file); readError != nil || string(contents) != "a file, not a directory" {
		test.Errorf("the file where the root goes was changed: %q (%v)", contents, readError)
	}
}

// tableIn is the one table the system cache at root holds, failing unless it holds exactly one, as a run over a fixture
// with no other build writes.
func tableIn(test *testing.T, root string) *cache.StandardLibraryTable {
	test.Helper()
	var tables []string
	for path := range systemCacheListing(test, standardLibraryTablesDirectory(root)) {
		tables = append(tables, path)
	}
	if len(tables) != 1 {
		test.Fatalf("want one table in the test's system cache, got %v", tables)
	}
	contents, readError := os.ReadFile(tables[0])
	if readError != nil {
		test.Fatal(readError)
	}
	// The store's file holds the table after the sha256 of it (cache.Store).
	return cache.NewStandardLibraryTable(contents[sha256.Size:])
}

// A cold run keeping a system cache writes the table of what it computed of the standard library, whole, and a warm
// run restores every package of it, its records those of a run without the system cache: the warm run computes no
// standard-library package the cold one computed whole, and finds what it would have found computing them.
func TestAColdRunWritesTheTableAWarmRunRestoresWhole(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	cold := runEngineWith(test, root, systemCache, rules.All(), "--no-cache")
	if cold.counts.FromSystemCache != 0 {
		test.Fatalf("want the cold run to restore nothing, got %+v", cold.counts)
	}
	table := tableIn(test, systemCache.systemCacheRoot)
	if problem := table.Problem(); problem != nil || len(table.Keys()) == 0 {
		test.Fatalf("want the cold run's table whole and holding its standard library, got %d keys (%v)",
			len(table.Keys()), problem)
	}
	warm := runEngineWith(test, root, systemCache, rules.All(), "--no-cache")
	if warm.counts.FromSystemCache != len(table.Keys()) {
		test.Errorf("want every one of the table's %d packages restored warm, got %+v", len(table.Keys()), warm.counts)
	}
	for name, ran := range map[string]cachedRun{"cold": cold, "warm": warm} {
		if ran.withoutCounts != plain.withoutCounts {
			test.Errorf("%s\n%s\nwithout the system cache\n%s", name, ran.withoutCounts, plain.withoutCounts)
		}
	}
}

// A warm run, which restores every standard-library package it reaches from the table and computes none it could
// add, writes nothing to the system cache: every file of it keeps its bytes and its time, the table's and the trim's
// record's alike, the table read too recently to be marked used again (cache.Store.MarkUsed).
func TestAWarmRunWritesNothingToTheSystemCache(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	runEngineWith(test, root, systemCache, rules.All())
	before := systemCacheListing(test, systemCache.systemCacheRoot)
	if len(before) == 0 {
		test.Fatal("the cold run wrote nothing to the system cache, so the warm run tells nothing")
	}
	// The modification times are the file system's, which a write in the same instant could leave equal.
	time.Sleep(10 * time.Millisecond)
	runEngineWith(test, root, systemCache, rules.All())
	after := systemCacheListing(test, systemCache.systemCacheRoot)
	if len(after) != len(before) || !equalEntries(before, after) {
		test.Errorf("want the system cache as the cold run left it, before %v, after %v", before, after)
	}
}

// A warm extra build hits in its own store the standard-library packages it checked cold only for the types of a
// package above them restored from the host's run, where no fact analyzer's action reached them: the system cache's
// table refuses such a short entry (check.standardLibraryEntryWhole), and the store takes it, as it took every
// standard-library entry before the system cache. The host's table is the generator's, some of whose entries carry no
// persisted reads, so the build declines those from the host and checks them, and what they import, from source. Over
// the fixture with a file only another target compiles, the run after a cold one, with that file edited so library
// misses in the build, checks library alone from source there, where a store that refused the short entries too would
// have the build check them from source again every run: 11 of them on darwin, beside library.
func TestAWarmExtraBuildHitsItsShortStandardLibraryEntriesInItsOwnStore(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	otherFile := filepath.Join(root, "library_"+other+".go")
	if writeError := os.WriteFile(otherFile, []byte("package library\n"), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	systemCache := generatedTableReading(test, localTable(test))
	cold := extraBuildRecords(test, runEngineWith(test, root, systemCache, rules.All(), "--other-builds").stream)
	if writeError := os.WriteFile(otherFile, []byte("// Package library is edited.\npackage library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	warm := extraBuildRecords(test, runEngineWith(test, root, systemCache, rules.All(), "--other-builds").stream)
	if len(warm) == 0 || len(warm) != len(cold) {
		test.Fatalf("want the warm run to run the cold run's %d extra builds, got %d", len(cold), len(warm))
	}
	for index, build := range warm {
		if cold[index].CheckedFromSource <= 1 {
			test.Fatalf("%s checked %d packages from source cold, so the warm run tells nothing", build.Target,
				cold[index].CheckedFromSource)
		}
		if build.CheckedFromSource != 1 || len(build.CheckedFromSourceRounds) > 0 {
			test.Errorf("%s checked %d packages from source warm over the rounds %v, of %d cold, want library alone",
				build.Target, build.CheckedFromSource, build.CheckedFromSourceRounds, cold[index].CheckedFromSource)
		}
	}
}
