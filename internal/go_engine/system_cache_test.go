package go_engine

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/cache"
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
