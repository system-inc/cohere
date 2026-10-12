package rule_testing

import (
	"fmt"
	"strings"
	"testing"
)

// withProgramCacheCapacity runs the test at a capacity of its own, restoring the package's after.
func withProgramCacheCapacity(t *testing.T, capacity int) {
	t.Helper()
	programCacheMutex.Lock()
	previous := programCacheCapacity
	programCacheCapacity = capacity
	evictBeyondCapacity()
	programCacheMutex.Unlock()
	t.Cleanup(func() {
		programCacheMutex.Lock()
		programCacheCapacity = previous
		programCacheMutex.Unlock()
	})
}

// cachedFixture builds the index'th of a family of distinct one-file fixtures through the cache.
func cachedFixture(t *testing.T, index int) string {
	t.Helper()
	files := map[string]string{"bound.ts": fmt.Sprintf("export const planted%d = %d;\n", index, index)}
	_, directory, err := buildCachedProgram(files, "bound.ts", false)
	if err != nil {
		t.Fatalf("building planted fixture %d: %v", index, err)
	}
	return directory
}

// cachedEntries is how many builds the cache holds, and how many its recency list orders.
func cachedEntries() (held int, ordered int) {
	programCacheMutex.Lock()
	defer programCacheMutex.Unlock()
	return len(programCache), programCacheRecency.Len()
}

// TestTheProgramCacheHoldsNoMoreThanItsBound is the guard on #7nx3z1n. Unbounded, the cache kept every
// fixture's graph for the life of the test binary, 69 GB at peak in registry. Planting three times the
// bound in distinct fixtures must leave the cache at the bound, the recency list in step with it, and
// the least recently used fixture the one gone.
//
// Shown able to fail: with eviction removed, it stops at the fifth planted fixture, 5 held against a
// bound of 4.
//
// Not parallel: it sets the package-wide cache capacity and counts what the shared cache holds, which
// another test building fixtures at the same time would change.
func TestTheProgramCacheHoldsNoMoreThanItsBound(t *testing.T) {
	const bound = 4
	withProgramCacheCapacity(t, bound)

	for index := range 3 * bound {
		cachedFixture(t, index)
		if held, ordered := cachedEntries(); held > bound || held != ordered {
			t.Fatalf("after %d planted fixtures the cache holds %d (its recency list %d) against a bound of %d",
				index+1, held, ordered, bound)
		}
	}

	// The last bound fixtures are the ones held, so asking for the newest again builds nothing new,
	// and the first is gone.
	builds := programCacheBuilds
	cachedFixture(t, 3*bound-1)
	if programCacheBuilds != builds {
		t.Fatal("the most recently used fixture was evicted instead of the least")
	}
	cachedFixture(t, 0)
	if programCacheBuilds != builds+1 {
		t.Fatal("the least recently used fixture was still held past the bound")
	}
}

// TestAFixtureHasOneRootWhateverWasBuiltBeforeIt is the guard on #jdkmmhs. A fixture's root is its key
// alone, so a fixture built first and the same fixture built again after another has evicted it get one
// root. A root that counted builds made every path a fixture reads, and every message naming one, follow
// t.Parallel's scheduling, which gave Adamic's traced builds a fresh key on every run for identical bytes.
//
// It also covers what sharing that root must not break. A test can still hold the graph the cache evicted,
// and the rebuild is a second graph at the same root, so the evicted graph must keep reading its own files.
// Each build reads a memory filesystem of its own, which nothing writes after it is made.
//
// Shown able to fail: with the root named fmt.Sprintf("%s-%d", key, programCacheBuilds) again, the second
// build's root differs from the first's.
//
// Not parallel: it sets the package-wide cache capacity to one, which would evict other tests' builds.
func TestAFixtureHasOneRootWhateverWasBuiltBeforeIt(t *testing.T) {
	withProgramCacheCapacity(t, 1)

	files := map[string]string{"bound.ts": "export const planted100 = 100;\n"}
	firstGraph, first, err := buildCachedProgram(files, "bound.ts", false)
	if err != nil {
		t.Fatal(err)
	}
	cachedFixture(t, 101)
	builds := programCacheBuilds
	secondGraph, second, err := buildCachedProgram(files, "bound.ts", false)
	if err != nil {
		t.Fatal(err)
	}

	// The second ask must be a real build, or the two roots agree because one entry answered both.
	if programCacheBuilds != builds+1 || secondGraph == firstGraph {
		t.Fatal("the fixture was not evicted and built again, so this proves nothing about its root")
	}
	if want := memoryFixtureRoot + "/" + programCacheKey(files, "bound.ts", false); first != want || second != want {
		t.Fatalf("a fixture's root is not its key alone: first build %s, after another fixture %s, want %s", first, second, want)
	}
	held := false
	for _, sourceFile := range firstGraph.ProjectFiles() {
		held = held || (sourceFile.FileName().AsString() == first+"/bound.ts" && strings.Contains(sourceFile.Text(), "planted100"))
	}
	if !held {
		t.Fatalf("the evicted graph no longer holds its own bound.ts under %s", first)
	}
}
