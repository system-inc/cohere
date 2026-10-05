package rule_testing

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/types/program"
)

// Building a type graph is the most expensive thing this harness does, and a build is memoized on
// exactly what determines its result: the fixture file set and the subject's name. Two calls with the
// same bytes cannot produce different graphs, which is what makes sharing one safe rather than merely
// fast.
//
// The memo is bounded, because unbounded it was the largest memory cost in the suite (#7nx3z1n): measured
// at GOMAXPROCS=3 it peaked at 69 GB in registry, 31 GB in rules/core and 29 GB in rules/typescript, past
// a 7 GB CI runner, which then swapped into the test timeout. Most of it held nothing a later test would
// ask for. Counted per package, rules/core asked 8,269 times for 6,923 distinct fixtures, and registry
// 20,046 times for 16,353, so nearly every entry was used once and kept forever.
//
// And every entry was heavy for a reason that had nothing to do with its fixture: each one-file program
// parsed the 58 lib files its ES2022 target names, 3.25ms and 2.5 MB of a build against 0.1ms for the
// fixture itself. So every build here, cached or not, takes its lib files from one shared parse
// (libraryParses, #0rj7wvg), and an entry is only what its fixture adds.
//
// Together, measured interleaved against the unbounded memo at GOMAXPROCS=3: every package peaks under
// 1.6 GB and runs no slower, registry 69 GB and 41s to 372 MB and 10s, rules/core 30 GB and 18.6s to
// 99 MB and 5.7s. The corpus walkers in `high_level_intermediate_representation` were the case a bound
// alone could not serve: thirty-seven walks of one pinned corpus ask 14,730 times for 1,529 fixtures,
// but each recurs only after the whole corpus has gone by, which no affordable LRU reaches. Bounded
// without the shared parse they ran 66s to about 93s; with it, 76.5s to 69.4s at 117 MB against 9.9 GB,
// because a rebuild now costs the fixture and not its libs.
//
// What is deliberately NOT shared is the checker. Upstream owns a checker per worker, keyed by the
// file's position in the program's file list, and it is thread-affine: reaching one from two
// goroutines produced `fatal error: concurrent map read and map write` inside `core.LinkStore.Get`,
// which is a runtime fatal rather than a recoverable panic. `checker_concurrency_test.go` records
// that in full. Callers keep going through `Graph.CheckerForFile`, which hands out the exclusive
// form and a release, so each test still acquires and releases its own. The cache holds the built
// graph; the checker lease stays per-test.

// programCacheEntry is one memoized build, with the error it produced if it failed.
//
// The error is cached alongside the graph so a fixture that cannot build fails the same way for
// every caller instead of being rebuilt on each attempt.
type programCacheEntry struct {
	once      sync.Once
	graph     *program.Graph
	buildErr  error
	directory string

	// key is the entry's key in programCache, and recency its place in programCacheRecency.
	key     string
	recency *list.Element
}

var (
	// libraryParses is the bundled lib files, parsed once for every program this test binary builds,
	// cached or not. Each fixture otherwise parsed its target's 58 lib files again, which was most of a
	// build's time and nearly all of what a built program kept (#7nx3z1n, #0rj7wvg).
	libraryParses = program.NewLibraryParses()

	programCacheMutex sync.Mutex
	programCache      = map[string]*programCacheEntry{}

	// programCacheRecency orders the entries most recently used first, so the least recently used is
	// the one evicted when the cache is full.
	programCacheRecency = list.New()

	// programCacheCapacity is how many built programs the cache holds at once.
	programCacheCapacity = defaultProgramCacheCapacity

	// programCacheBuilds counts builds, so each one writes a directory of its own. A key evicted and
	// built again must not rewrite the directory the first build used: a test still holding that
	// graph may read its files later, since the compiler reads lazily.
	programCacheBuilds int

	// One directory for every cached fixture, not `t.TempDir()`.
	//
	// This is the reason the cache needs a root of its own. `t.TempDir()` is removed when the test
	// that asked for it finishes, and a graph outliving that test would then hold paths to files
	// that no longer exist. The compiler reads lazily, so the failure would not appear at build time
	// but later, in whichever unrelated test happened to reuse the entry.
	programCacheRootOnce sync.Once
	programCacheRoot     string
	programCacheRootErr  error
)

// RunTestsAndCleanUp is the TestMain body for a package that uses the typed harness.
//
// Every package whose tests build a type graph shares one on-disk fixture cache, and that cache has
// to be swept when the test binary exits. Packages delegate here rather than each writing the same
// run-then-remove dance, so the sweep cannot drift between them:
//
//	func TestMain(m *testing.M) { rule_testing.RunTestsAndCleanUp(m) }
//
// The cleanup runs before the exit code is handed back, and it runs whatever the outcome, so a
// failing suite does not leave its fixtures behind.
func RunTestsAndCleanUp(m *testing.M) {
	code := m.Run()
	CleanUpCachedFixtures()
	os.Exit(code)
}

// CleanUpCachedFixtures removes everything the cache wrote to disk.
//
// This exists because the cached fixtures outlive the test that first asked for them, which is the
// whole point of the cache and also the reason nothing else will clean them up. `t.TempDir()` is
// removed when its test ends; a directory shared by every test in the binary has no such moment, so
// without this each run of each package would leave its fixtures behind forever. Measured at 494MB
// across twenty-seven runs before this was added.
//
// Call it from `TestMain` after `m.Run()`. It is safe to call when the cache was never used.
func CleanUpCachedFixtures() {
	programCacheMutex.Lock()
	defer programCacheMutex.Unlock()

	if programCacheRoot != "" {
		os.RemoveAll(programCacheRoot)
	}
}

// cacheRoot returns the directory cached fixtures are written under, creating it once.
func cacheRoot() (string, error) {
	programCacheRootOnce.Do(func() {
		programCacheRoot, programCacheRootErr = os.MkdirTemp("", "cohere-rule-testing-*")
	})
	return programCacheRoot, programCacheRootErr
}

// programCacheKey hashes everything that can change what a build produces.
//
// The file names are sorted so a caller's map iteration order cannot split one program into two
// cache entries, and each field is length-prefixed so that no combination of names and contents can
// be spelled two ways and collide.
func programCacheKey(files map[string]string, subjectFileName string, verbatim bool) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	hash := sha256.New()
	fmt.Fprintf(hash, "subject:%d:%s\n", len(subjectFileName), subjectFileName)
	fmt.Fprintf(hash, "verbatim:%t\n", verbatim)
	for _, name := range names {
		fmt.Fprintf(hash, "file:%d:%s:%d:%s\n", len(name), name, len(files[name]), files[name])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// buildCachedProgram returns the graph for a fixture set, building it at most once per distinct set.
//
// The returned directory is where the fixture was written, for a caller that needs to name a path
// inside it. It is owned by the cache and must not be modified: another test is very likely holding
// the same graph.
func buildCachedProgram(files map[string]string, subjectFileName string, verbatim bool) (*program.Graph, string, error) {
	key := programCacheKey(files, subjectFileName, verbatim)

	programCacheMutex.Lock()
	entry, found := programCache[key]
	if found {
		programCacheRecency.MoveToFront(entry.recency)
	} else {
		programCacheBuilds++
		entry = &programCacheEntry{key: key, directory: fmt.Sprintf("%s-%d", key, programCacheBuilds)}
		entry.recency = programCacheRecency.PushFront(entry)
		programCache[key] = entry
		evictBeyondCapacity()
	}
	programCacheMutex.Unlock()

	// Outside the map lock, so one slow build does not stall every other fixture, and `sync.Once`
	// still guarantees exactly one build per key even when several tests race for the same one.
	entry.once.Do(func() {
		entry.graph, entry.directory, entry.buildErr = buildProgramInto(entry.directory, files, subjectFileName, verbatim)
	})

	return entry.graph, entry.directory, entry.buildErr
}

// defaultProgramCacheCapacity is how many built programs a test binary keeps. Sixteen, chosen by
// measurement before the shared lib parse (#7nx3z1n), when an entry still held its libs: every rule
// package and registry peaked under 1 GB at sixteen and ran no slower than unbounded, while rules/nexus,
// whose fixtures are the largest, reached 2.1 GB at sixty-four, and the corpus walkers ran no faster at
// 128 than at sixteen. With the shared parse an entry is far smaller, rules/nexus peaking at 151 MB, so
// sixteen now has room; it was not raised, because no package measured slower at it.
const defaultProgramCacheCapacity = 16

// evictBeyondCapacity drops the least recently used entries until the cache is within its capacity.
// The caller holds programCacheMutex. An evicted graph lives on in any test still holding it, and is
// collected when the last one lets go; its files stay on disk until the binary exits.
func evictBeyondCapacity() {
	for len(programCache) > programCacheCapacity {
		oldest := programCacheRecency.Back()
		evicted := oldest.Value.(*programCacheEntry)
		programCacheRecency.Remove(oldest)
		delete(programCache, evicted.key)
	}
}

// buildProgramInto writes one fixture set to its own directory under the cache root, named directoryName,
// and builds it.
func buildProgramInto(directoryName string, files map[string]string, subjectFileName string, verbatim bool) (*program.Graph, string, error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, "", fmt.Errorf("creating the fixture cache root: %w", err)
	}

	directory := filepath.Join(root, directoryName)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, "", fmt.Errorf("creating the fixture directory: %w", err)
	}

	for name, contents := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, "", fmt.Errorf("creating the fixture directory for %s: %w", name, err)
		}
		text := FixtureText(contents)
		if verbatim {
			text = contents
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			return nil, "", fmt.Errorf("writing the fixture %s: %w", name, err)
		}
	}

	configPath := filepath.Join(directory, "tsconfig.json")
	if err := os.WriteFile(configPath, []byte(defaultTsConfig), 0o644); err != nil {
		return nil, "", fmt.Errorf("writing the tsconfig: %w", err)
	}

	graph, err := program.Build(program.Options{
		ConfigFileName:   configPath,
		CurrentDirectory: directory,
		LibraryParses:    libraryParses,
		// One checker rather than several: a fixture is one file, and the parallel path adds
		// scheduling nondeterminism to a test whose whole value is being deterministic.
		SingleThreaded: true,
	})
	if err != nil {
		return nil, directory, fmt.Errorf("building the type graph: %w", err)
	}
	return graph, directory, nil
}
