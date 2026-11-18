// Package system_cache_testing keeps a test binary, and every process it starts, out of the person's own system cache
// (#syscache): the Go engine's standard-library tables, which a run reads from the directory `COHERE_SYSTEM_CACHE`
// names, or else from the user's cache directory. A package whose tests run the engine, in process or as a binary,
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

// Variable is the environment variable the engine reads the system cache's root from, spelled as the environment
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

// ExpectIsolated fails the test unless Variable names an absolute directory outside usersCache, the person's user
// cache directory as it was before the tests moved anything, empty when the machine has none.
func ExpectIsolated(test *testing.T, usersCache string) {
	test.Helper()
	systemCache := os.Getenv(Variable)
	if systemCache == "" || !filepath.IsAbs(systemCache) {
		test.Fatalf("want %s set to an absolute directory of the tests' own, got %q, so the tests read the "+
			"person's system cache: call system_cache_testing.Isolate from TestMain", Variable, systemCache)
	}
	if usersCache == "" {
		return
	}
	relative, relativeError := filepath.Rel(usersCache, systemCache)
	if relativeError == nil && (relative == "." || !strings.HasPrefix(relative, "..")) {
		test.Errorf("%s %s is in the user cache directory %s", Variable, systemCache, usersCache)
	}
}
