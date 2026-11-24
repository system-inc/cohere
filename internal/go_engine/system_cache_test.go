package go_engine

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
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

// testsEnvironment is the process's environment as TestMain left it, sorted, which the system cache's template is
// filled under (seededSystemCache).
var testsEnvironment []string

// sealedGitEnvironment is what TestMain sets so that git reads no configuration but a repository's own: no global
// file, and no system one.
var sealedGitEnvironment = map[string]string{"GIT_CONFIG_GLOBAL": os.DevNull, "GIT_CONFIG_NOSYSTEM": "1"}

func TestMain(suite *testing.M) {
	if os.Getenv(timingMarginChildVariable) != "" {
		os.Exit(runTimingMarginChild(os.Args[1:], os.Stdout, os.Stderr))
	}
	// Every run in this process, and every child it starts, reads a system cache of this test binary's own rather
	// than the person's (#syscache). The engine's test helpers give each run a system cache of its own beside that
	// (isolatedSource), and a test that names one on the run (standardLibrarySource's systemCacheRoot), or sets the
	// variable itself, reads that one.
	removeSystemCache, isolateError := system_cache_testing.Isolate()
	if isolateError != nil {
		os.Exit(refuse(os.Stderr, isolateError))
	}
	testsSystemCache = os.Getenv(system_cache_testing.Variable)
	// And no go command of this process or its children reads the person's go env file (`go env -w`), whose
	// `GOFLAGS=-mod=mod` or `GOAMD64` would change what the engine's go commands answer and redden tests that never
	// asked for it (#sfjj2rh, the reader's S6). A test about the file sets one of its own.
	if setError := os.Setenv("GOENV", "off"); setError != nil {
		os.Exit(refuse(os.Stderr, setError))
	}
	// And no git child of this process or its runs, the engine's own `git ls-files` at a go.work's root among them,
	// reads the machine's git configuration (#2htryg8): a person's global config signs, hooks and ignores as the
	// person set it, and a test's git reads the repository's own alone. Set before testsEnvironment is taken, so
	// the template and every guard that compares the environment with it hold these too.
	for name, value := range sealedGitEnvironment {
		if setError := os.Setenv(name, value); setError != nil {
			os.Exit(refuse(os.Stderr, setError))
		}
	}
	testsEnvironment = slices.Sorted(slices.Values(os.Environ()))
	code := suite.Run()
	removeSystemCache()
	os.Exit(code)
}

// testsGoAnswers are go env's and `go tool dist list`'s answers this test binary keeps (load.GoAnswers), which every
// run isolatedSource gives a source reads under TestMain's environment (#2htryg8).
var testsGoAnswers = load.NewGoAnswers()

// isolatedSource is source with a new system cache of the run's own, seeded from the template (seededSystemCache),
// unless source names one or the test set the variable to one of its own, which the run then reads. Each run of a test
// that names none starts from the same system cache, the template's, whatever ran before, and writes what it computes
// where no other run reads it: tests run side by side, and a test that compares two of its own runs, or counts what one
// restored, would otherwise read tables another run wrote, its counts turning on which ran first (#2htryg8). A test of
// what a cold system cache does has every run of it read an empty one of its own (coldSystemCacheFor), and a test of
// what a warm one does names one shared by the runs it compares (sharingSystemCache). The source also reads the go
// answers the process keeps (testsGoAnswers), so a run under what decided an answer before forks no go env or
// `go tool dist list` for it.
func isolatedSource(test *testing.T, source standardLibrarySource) standardLibrarySource {
	test.Helper()
	if source.systemCacheRoot == "" && os.Getenv(system_cache_testing.Variable) == testsSystemCache {
		source.systemCacheRoot = seededSystemCache(test)
	}
	// Under TestMain's environment alone, as the template is: a test that sets `GOROOT` or `PATH` may change the
	// toolchain's files below the same paths between its runs, which no key holds.
	if source.goAnswers == nil && slices.Equal(slices.Sorted(slices.Values(os.Environ())), testsEnvironment) {
		source.goAnswers = testsGoAnswers
	}
	return source
}

// coldTests are the tests every run of which that names no system cache reads a new, empty one of its own rather
// than one seeded from the template (coldSystemCacheFor), each test by itself.
var coldTests sync.Map

// coldSystemCacheFor has every run of test that names no system cache read and write a new, empty one of its own, as
// every run did before the template (#2htryg8): for a test of what a run computing the standard library does, which
// the template's entries would restore instead, or one that holds a run restoring it to a run computing it. It is
// test's alone, a subtest's apart from its parent's, and test's cleanup forgets it. A new test of the cold path opts
// in here: one left unmarked reads the template and passes warm, never seeing the path it means to test.
func coldSystemCacheFor(test *testing.T) {
	test.Helper()
	coldTests.Store(test, true)
	test.Cleanup(func() { coldTests.Delete(test) })
}

// sharingSystemCache has a pipeline read from and write to the system cache at root, which every run of a test given
// the same root shares, as a person's runs share theirs.
func sharingSystemCache(root string) func(*pipeline) {
	return func(engine *pipeline) {
		sharing := engine.request.standardLibrary
		sharing.systemCacheRoot = root
		engine.request.standardLibrary = sharing
		engine.systemCache = openSystemCache(engine.request, engine.shardStamps)
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
		engine.systemCache = openSystemCache(engine.request, engine.shardStamps)
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

// A plain run, its standard library the zero source, reads and writes the system cache `$COHERE_SYSTEM_CACHE` names:
// a cold run under `--no-cache` writes there every standard-library package it computes, keyed by the engine's own
// identity, and every later run restores each one, cached and under `--no-cache` alike, the records those of a run
// without the system cache.
//
// Not parallel: it sets `COHERE_SYSTEM_CACHE`, which test.Setenv refuses beside parallel tests.
func TestARunRestoresTheStandardLibraryFromTheSystemCacheItNames(test *testing.T) {
	systemCache := test.TempDir()
	test.Setenv("COHERE_SYSTEM_CACHE", systemCache)
	root := copyFixture(test, standardLibraryFixture)
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	cold := runCachedEngine(test, root, "--no-cache")
	if cold.counts.FromSystemCache != 0 || cold.counts.SystemCacheWritten == 0 {
		test.Fatalf("want the cold run to write the standard library to the system cache it names, got %+v",
			cold.counts)
	}
	if listing := systemCacheListing(test, systemStoreDirectory(systemCache)); len(listing) == 0 {
		test.Fatal("want the entries under the system cache's store")
	}
	// Under `--no-cache` first, which makes no store, so the cached run after it is cold for the project.
	for _, extra := range [][]string{{"--no-cache"}, {}} {
		read := runCachedEngine(test, root, extra...)
		if read.counts.FromSystemCache != cold.counts.SystemCacheWritten || read.counts.SystemCacheNotKept != "" {
			test.Errorf("%v: want all %d standard-library packages from the system cache it names, got %+v", extra,
				cold.counts.SystemCacheWritten, read.counts)
		}
		if read.withoutCounts != plain.withoutCounts {
			test.Errorf("%v: from the system cache\n%s\nwithout it\n%s", extra, read.withoutCounts,
				plain.withoutCounts)
		}
	}
}

// `--no-cache` turns off the project's store and nothing else: the run still reads the system cache, restoring every
// standard-library package from its store, and makes no store in the project.
func TestNoCacheStillReadsTheSystemCache(test *testing.T) {
	test.Parallel()

	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	root := copyFixture(test, standardLibraryFixture)
	read := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if read.counts.On || read.counts.FromSystemCache != written {
		test.Errorf("want the store off and all %d standard-library packages from the system cache, got %+v",
			written, read.counts)
	}
	if _, statError := os.Stat(filepath.Join(root, ".cache")); !os.IsNotExist(statError) {
		test.Errorf("want no store made under --no-cache, got %v", statError)
	}
}

// `--no-system-cache` neither reads nor writes the system cache: a run under it over a system cache holding the
// fixture's standard library, every file an hour and more old so a read would mark it used, and due a trim, restores
// nothing from it, and leaves every file of it as it was, the times and the absent trim record among them. Its
// records are those of a run reading the system cache.
func TestNoSystemCacheLeavesTheSystemCacheAsItWas(test *testing.T) {
	test.Parallel()

	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	aged := time.Now().Add(-2 * time.Hour)
	for path := range systemCacheListing(test, reading.systemCacheRoot) {
		if timesError := os.Chtimes(path, aged, aged); timesError != nil {
			test.Fatal(timesError)
		}
	}
	if removeError := os.Remove(filepath.Join(reading.systemCacheRoot, "trim.txt")); removeError != nil &&
		!errors.Is(removeError, fs.ErrNotExist) {
		test.Fatal(removeError)
	}
	before := systemCacheListing(test, reading.systemCacheRoot)
	root := copyFixture(test, standardLibraryFixture)
	off := runEngineWith(test, root, reading, rules.All(), "--no-cache", "--no-system-cache")
	if off.counts.FromSystemCache != 0 || off.counts.SystemCacheWritten != 0 || off.counts.SystemCacheNotKept != "" {
		test.Errorf("want nothing from or to the system cache under --no-system-cache, got %+v", off.counts)
	}
	after := systemCacheListing(test, reading.systemCacheRoot)
	if len(after) != len(before) || !equalEntries(before, after) {
		test.Errorf("want the system cache as it was under --no-system-cache, before %v, after %v", before, after)
	}
	on := runEngineWith(test, root, reading, rules.All(), "--no-cache")
	if on.counts.FromSystemCache != written || on.withoutCounts != off.withoutCounts {
		test.Errorf("want all %d packages from the system cache without the switch, and the same records, got %+v\n"+
			"%s\nunder the switch\n%s", written, on.counts, on.withoutCounts, off.withoutCounts)
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

// A cold run keeping a system cache writes there every standard-library package it computes, and a warm run restores
// every one of them, its records those of a run without the system cache: the warm run computes no standard-library
// package the cold one wrote, and finds what it would have found computing them.
func TestAColdRunWritesTheSystemCacheAWarmRunRestoresIt(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	plain := runCachedEngine(test, root, "--no-cache", "--no-system-cache")
	cold := runEngineWith(test, root, systemCache, rules.All(), "--no-cache")
	if cold.counts.FromSystemCache != 0 || cold.counts.SystemCacheWritten == 0 {
		test.Fatalf("want the cold run to restore nothing and write the standard library, got %+v", cold.counts)
	}
	warm := runEngineWith(test, root, systemCache, rules.All(), "--no-cache")
	if warm.counts.FromSystemCache != cold.counts.SystemCacheWritten || warm.counts.SystemCacheWritten != 0 {
		test.Errorf("want every one of the %d packages written cold restored warm, and none written, got %+v",
			cold.counts.SystemCacheWritten, warm.counts)
	}
	for name, ran := range map[string]cachedRun{"cold": cold, "warm": warm} {
		if ran.withoutCounts != plain.withoutCounts {
			test.Errorf("%s\n%s\nwithout the system cache\n%s", name, ran.withoutCounts, plain.withoutCounts)
		}
	}
}

// A warm run, which restores every standard-library package it reaches from the system cache and computes none,
// writes nothing to it: every file of it keeps its bytes and its time, the entries' and the trim's record's alike,
// each read too recently to be marked used again (cache.Store.MarkUsed).
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

// Every build's record says what the system cache did for it, and the lint record sums them (#syscache): over a
// fixture with a file only another target compiles, a cold run's host build writes the standard library to it and
// restores nothing, a warm run's host restores every package it wrote and writes nothing, the lint record's counts are
// the builds' summed,
// and a run under `--no-system-cache` says the system cache was off and counts nothing of it.
func TestTheRecordsSayWhatTheSystemCacheDid(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	if writeError := os.WriteFile(filepath.Join(root, "library_"+other+".go"), []byte("package library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	summed := func(lint contract.LintRecord) (int, int) {
		restored, written := 0, 0
		for _, build := range lint.Builds {
			restored, written = restored+build.SystemCacheRestored, written+build.SystemCacheWritten
		}
		return restored, written
	}
	cold := lintRecordOf(test, runEngineWith(test, root, systemCache, rules.All(), "--no-cache").stream)
	if len(cold.Builds) < 2 || cold.Builds[0].SystemCacheWritten == 0 || cold.Builds[0].SystemCacheRestored != 0 {
		test.Fatalf("want the cold host to write to the system cache and restore nothing, got %+v", cold.Builds)
	}
	warm := lintRecordOf(test, runEngineWith(test, root, systemCache, rules.All(), "--no-cache").stream)
	if warm.Builds[0].SystemCacheRestored != cold.Builds[0].SystemCacheWritten ||
		warm.Builds[0].SystemCacheWritten != 0 {
		test.Errorf("want the warm host to restore the %d entries the cold one wrote and write none, got %+v",
			cold.Builds[0].SystemCacheWritten, warm.Builds[0])
	}
	for name, lint := range map[string]contract.LintRecord{"cold": cold, "warm": warm} {
		restored, written := summed(lint)
		if lint.Cache.FromSystemCache != restored || lint.Cache.SystemCacheWritten != written ||
			lint.Cache.SystemCacheOff {
			test.Errorf("%s: want the lint record to sum the builds' %d restored and %d written, got %+v", name,
				restored, written, lint.Cache)
		}
	}
	off := lintRecordOf(test, runEngineWith(test, root, systemCache, rules.All(), "--no-cache",
		"--no-system-cache").stream)
	if restored, written := summed(off); !off.Cache.SystemCacheOff || off.Cache.FromSystemCache != 0 ||
		restored+written != 0 {
		test.Errorf("want a run under --no-system-cache to say the system cache was off and count nothing of it, "+
			"got %+v and builds %+v", off.Cache, off.Builds)
	}
}

// A warm extra build of another target restores from the system cache's store every standard-library package it
// computed or wrote through from the host's run cold, under its own context's keys, and checks none of them from source
// (#syscache's must): over the fixture with a file only another target compiles, a cold run under `--no-cache` has the
// build write its standard library to the system cache, and the warm run's build restores all of it, checks the
// module's own package alone from source, declines nothing at the check and loads no second round. A build whose
// standard-library writes went nowhere, or to a project's store, would check them from source again.
func TestAWarmExtraBuildRestoresTheStandardLibraryItWroteFromTheSystemCache(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, standardLibraryFixture)
	other := map[bool]string{true: "freebsd", false: "linux"}[runtime.GOOS == "linux"]
	if writeError := os.WriteFile(filepath.Join(root, "library_"+other+".go"), []byte("package library\n"),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	systemCache := standardLibrarySource{systemCacheRoot: test.TempDir()}
	cold := lintRecordOf(test, runEngineWith(test, root, systemCache, rules.All(), "--no-cache").stream)
	warm := lintRecordOf(test, runEngineWith(test, root, systemCache, rules.All(), "--no-cache").stream)
	if len(warm.Builds) < 2 || len(warm.Builds) != len(cold.Builds) {
		test.Fatalf("want the warm run to run the cold run's builds and one beside the host's, got %+v", warm.Builds)
	}
	for index, build := range warm.Builds[1:] {
		coldBuild := cold.Builds[index+1]
		if coldBuild.SystemCacheWritten == 0 {
			test.Errorf("%s wrote nothing to the system cache cold, %+v", coldBuild.Target, coldBuild)
		}
		if build.SystemCacheRestored != coldBuild.SystemCacheWritten || build.CheckedFromSource != 1 ||
			build.DeclinedAtCheck != 0 || build.ReloadRounds != 0 {
			test.Errorf("%s warm restored %d of the %d it wrote cold, checked %d from source, declined %d at the "+
				"check over %d reloads, want all restored, the module's package alone checked and nothing declined: "+
				"%+v", build.Target, build.SystemCacheRestored, coldBuild.SystemCacheWritten, build.CheckedFromSource,
				build.DeclinedAtCheck, build.ReloadRounds, build)
		}
	}
}

// A run marks every system cache file it reads used, so the trim, which deletes what has gone unread for days, keeps
// what runs read every day: over a system cache filled by a cold run, its files made two hours old, a warm run that
// restores the whole standard library from it leaves every file of its store newer than that.
func TestAWarmRunMarksTheSystemCacheItReadsUsed(test *testing.T) {
	test.Parallel()

	reading, written := filledSystemCache(test, rules.All(), engineTestsStamp, nil)
	store := systemStoreDirectory(reading.systemCacheRoot)
	aged := time.Now().Add(-2 * time.Hour)
	for path := range systemCacheListing(test, store) {
		if timesError := os.Chtimes(path, aged, aged); timesError != nil {
			test.Fatal(timesError)
		}
	}
	warm := runEngineWith(test, copyFixture(test, standardLibraryFixture), reading, rules.All(), "--no-cache")
	if warm.counts.FromSystemCache != written {
		test.Fatalf("want all %d packages restored from the system cache, got %+v", written, warm.counts)
	}
	for path := range systemCacheListing(test, store) {
		information, statError := os.Stat(path)
		if statError != nil {
			test.Fatal(statError)
		}
		if !information.ModTime().After(aged) {
			test.Errorf("%s was read and left unmarked, so a trim would take it", path)
		}
	}
}

// A write to the system cache's store that fails is why the run says it could not keep the system cache, never an
// error and never the project's problem: over a system cache whose store cannot be written, and whose trim is not due,
// so it gives no reason of its own, a cold run under `--no-cache` says the system cache was not kept, once, and exits
// as a run without it does.
func TestAFailedSystemCacheWriteIsWhyItWasNotKept(test *testing.T) {
	test.Parallel()

	root := test.TempDir()
	store := systemStoreDirectory(root)
	if makeError := os.MkdirAll(store, 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	// Trimmed a moment ago, so the trim is not due and says nothing.
	if writeError := os.WriteFile(filepath.Join(root, "trim.txt"), []byte(strconv.FormatInt(time.Now().Unix(), 10)),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if modeError := os.Chmod(store, 0o500); modeError != nil {
		test.Fatal(modeError)
	}
	test.Cleanup(func() {
		// Ignored: the directory goes with the test's own, which a failure here only leaves behind.
		_ = os.Chmod(store, 0o755)
	})
	project := copyFixture(test, standardLibraryFixture)
	plain := runEngineWith(test, project, standardLibrarySource{}, rules.All(), "--no-cache", "--no-system-cache")
	unwritable, standardError := runEngineNoting(test, project, standardLibrarySource{systemCacheRoot: root},
		rules.All(), nil, "--no-cache")
	if reason := unwritable.counts.SystemCacheNotKept; reason == "" || unwritable.counts.SystemCacheWritten != 0 ||
		unwritable.counts.Problems != 0 {
		test.Errorf("want the failed write as why the system cache was not kept, nothing written and no problem, "+
			"got %+v", unwritable.counts)
	}
	if strings.Count(standardError, "the system cache was not kept") != 1 {
		test.Errorf("want stderr to say once that the system cache was not kept, got %q", standardError)
	}
	if unwritable.exitCode != plain.exitCode || unwritable.withoutCounts != plain.withoutCounts {
		test.Errorf("over an unwritable store the run exited %d, want %d, or its records differ:\n%s\nplain\n%s",
			unwritable.exitCode, plain.exitCode, unwritable.withoutCounts, plain.withoutCounts)
	}
}

// One project's standard-library entries serve another's (#znqfjw2): the system cache is keyed by the engine, the
// standard library's build and its facts, never by the project, so a second module on the same machine restores from
// the store the first one filled every package both reach, computes only those the first never reached, and reads as
// it does with no system cache at all. The second module reaches `text/tabwriter`, which the first fixture never
// imports, so the run has something of its own to write.
func TestOneProjectsStandardLibraryEntriesServeAnothers(test *testing.T) {
	test.Parallel()
	shared := standardLibrarySource{systemCacheRoot: test.TempDir()}
	first := runEngineWith(test, copyFixture(test, standardLibraryFixture), shared, rules.All(), "--no-cache")
	if first.counts.SystemCacheWritten == 0 {
		test.Fatalf("want the first project to fill the system cache, got %+v", first.counts)
	}

	second := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod": "module example.com/secondproject\n\ngo 1.24\n",
		"second.go": "// Package second writes a table, and holds printf's finding of a verb that does not fit.\n" +
			"package second\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"strings\"\n\t\"text/tabwriter\"\n)\n\n" +
			"// Print writes name under a header, padded.\nfunc Print(name string) {\n" +
			"\twriter := tabwriter.NewWriter(os.Stdout, 0, 4, 1, ' ', 0)\n" +
			"\tfmt.Fprintf(writer, \"%d\\t%s\\n\", strings.ToUpper(name), name)\n" +
			"\t// Ignored: a fixture's flush has nowhere to report to.\n\t_ = writer.Flush()\n}\n",
	} {
		if writeError := os.WriteFile(filepath.Join(second, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	alone := runEngineWith(test, second, standardLibrarySource{}, rules.All(), "--no-cache", "--no-system-cache")
	served := runEngineWith(test, second, shared, rules.All(), "--no-cache")
	if served.counts.FromSystemCache == 0 || served.counts.SystemCacheWritten == 0 {
		test.Errorf("want the second project to restore what the first wrote and write what only it reaches, "+
			"got %+v", served.counts)
	}
	if !strings.Contains(alone.stream, "cohere-go/printf") {
		test.Fatalf("the second project's run found no printf finding, so it tells nothing:\n%s", alone.stream)
	}
	if served.withoutCounts != alone.withoutCounts {
		test.Errorf("the second project served by the first's entries\n%s\nwith no system cache\n%s",
			served.withoutCounts, alone.withoutCounts)
	}
}
