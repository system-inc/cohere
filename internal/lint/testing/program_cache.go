package rule_testing

import (
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

// Building a type graph is the single most expensive thing this harness does, roughly a second a
// call, and the suite calls it about ten thousand times. Almost none of those builds are distinct.
//
// The corpus walkers in `high_level_intermediate_representation` are the extreme case: twenty-five
// tests each walk the same tree, read the same four hundred files, and build a one-file program per
// file. That is the identical program built twenty-five times over, because the fixture text is the
// same bytes every time. The rules packages have the same shape more diffusely, where many rules are
// fixtured against the same small snippets.
//
// So a build is memoized on exactly what determines its result: the fixture file set and the
// subject's name. Two calls with the same bytes cannot produce different graphs, which is what makes
// this safe to share rather than merely fast.
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
}

var (
	programCacheMutex sync.Mutex
	programCache      = map[string]*programCacheEntry{}

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
func programCacheKey(files map[string]string, subjectFileName string) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)

	hash := sha256.New()
	fmt.Fprintf(hash, "subject:%d:%s\n", len(subjectFileName), subjectFileName)
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
func buildCachedProgram(files map[string]string, subjectFileName string) (*program.Graph, string, error) {
	key := programCacheKey(files, subjectFileName)

	programCacheMutex.Lock()
	entry, found := programCache[key]
	if !found {
		entry = &programCacheEntry{}
		programCache[key] = entry
	}
	programCacheMutex.Unlock()

	// Outside the map lock, so one slow build does not stall every other fixture, and `sync.Once`
	// still guarantees exactly one build per key even when several tests race for the same one.
	entry.once.Do(func() {
		entry.graph, entry.directory, entry.buildErr = buildProgramInto(key, files, subjectFileName)
	})

	return entry.graph, entry.directory, entry.buildErr
}

// buildProgramInto writes one fixture set to its own directory under the cache root and builds it.
func buildProgramInto(key string, files map[string]string, subjectFileName string) (*program.Graph, string, error) {
	root, err := cacheRoot()
	if err != nil {
		return nil, "", fmt.Errorf("creating the fixture cache root: %w", err)
	}

	directory := filepath.Join(root, key)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, "", fmt.Errorf("creating the fixture directory: %w", err)
	}

	for name, contents := range files {
		path := filepath.Join(directory, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, "", fmt.Errorf("creating the fixture directory for %s: %w", name, err)
		}
		if err := os.WriteFile(path, []byte(FixtureText(contents)), 0o644); err != nil {
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
		// One checker rather than several: a fixture is one file, and the parallel path adds
		// scheduling nondeterminism to a test whose whole value is being deterministic.
		SingleThreaded: true,
	})
	if err != nil {
		return nil, directory, fmt.Errorf("building the type graph: %w", err)
	}
	return graph, directory, nil
}
