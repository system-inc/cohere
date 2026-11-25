// Package system_cache_testing keeps a test binary, and every process it starts, out of the person's own system cache
// (#syscache): cohere-go's standard-library tables, which a run reads from the directory `COHERE_SYSTEM_CACHE`
// names, or else from the user's cache directory. A package whose tests run cohere-go, in process or as a binary,
// calls Isolate from its TestMain and ExpectIsolated from a test of its own, so a TestMain that lost the call fails
// rather than reads, trims or writes the person's cache.
package system_cache_testing

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Variable is the environment variable cohere-go reads the system cache's root from, spelled as the environment
// spells it.
const Variable = "COHERE_SYSTEM_CACHE"

// Isolate points Variable at a new temporary directory, for this process and every process it starts, which inherit
// it, and returns the function that removes the directory once the tests have run.
func Isolate() (func(), error) {
	directory, temporaryDirectoryError := os.MkdirTemp("", "cohere-test-system-cache-")
	if temporaryDirectoryError != nil {
		return nil, fmt.Errorf("making the tests' system cache: %w", temporaryDirectoryError)
	}
	if setError := os.Setenv(Variable, directory); setError != nil {
		return nil, fmt.Errorf("pointing %s at the tests' system cache: %w", Variable, setError)
	}
	return func() {
		// Ignored: a temporary directory of this test binary's own, which costs only its space if it outlives the
		// run, and the run's verdict is already decided.
		_ = os.RemoveAll(directory)
	}, nil
}

// PersonsSystemCache is the system cache a run on this machine reads when `COHERE_SYSTEM_CACHE` is unset, from
// usersCache, the person's user cache directory: usersCache joined with `cohere`, as cohere-go's
// cache.SystemCacheRoot spells it, which a test of the cache package holds this to. Empty when usersCache is.
func PersonsSystemCache(usersCache string) string {
	if usersCache == "" {
		return ""
	}
	return filepath.Join(usersCache, "cohere")
}

// ExpectIsolated fails the test unless Variable names an absolute directory that is neither the person's system
// cache (PersonsSystemCache of usersCache, the person's user cache directory as it was before the tests moved
// anything, empty when the machine has none) nor inside it, nor holds it.
func ExpectIsolated(test *testing.T, usersCache string) {
	test.Helper()
	if problem := isolationProblem(os.Getenv(Variable), usersCache); problem != "" {
		test.Fatal(problem)
	}
}

// isolationProblem is why systemCache, the tests' system cache as Variable names it, is not isolated from the
// person's, or empty when it is. Only the person's own system cache is guarded, never the whole user cache directory:
// a machine whose temporary directory sits under it, as Loom's keeper puts a job's under `~/.cache` (#rnw5f6h), makes
// the tests' cache there, apart from the person's.
func isolationProblem(systemCache string, usersCache string) string {
	if systemCache == "" || !filepath.IsAbs(systemCache) {
		return fmt.Sprintf("want %s set to an absolute directory of the tests' own, got %q, so the tests read the "+
			"person's system cache: call system_cache_testing.Isolate from TestMain", Variable, systemCache)
	}
	persons := PersonsSystemCache(usersCache)
	if persons == "" {
		return ""
	}
	if within(persons, systemCache) || within(systemCache, persons) {
		return fmt.Sprintf("%s %s is the person's system cache %s, inside it or holding it", Variable, systemCache,
			persons)
	}
	return ""
}

// within reports whether path is directory or inside it.
func within(directory string, path string) bool {
	relative, relativeError := filepath.Rel(directory, path)
	return relativeError == nil && (relative == "." || !strings.HasPrefix(relative, ".."))
}
