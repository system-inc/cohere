package system_cache_testing

import (
	"path/filepath"
	"testing"
)

// Only the person's own system cache is guarded (#rnw5f6h): a tests' cache under the user cache directory but apart
// from its cohere directory is isolated, as on a Loom keeper whose temporary directory is under `~/.cache`, while one
// that is the person's, inside it or holding it is not.
func TestOnlyThePersonsOwnSystemCacheIsRefused(test *testing.T) {
	test.Parallel()

	usersCache := filepath.Join(string(filepath.Separator), "home", "person", ".cache")
	for _, each := range []struct {
		name        string
		systemCache string
		isolated    bool
	}{
		{name: "a keeper's temporary directory under the user cache", isolated: true,
			systemCache: filepath.Join(usersCache, "loom-keeper-tmp", "job", "cohere-test-system-cache-1")},
		{name: "elsewhere", systemCache: filepath.Join(string(filepath.Separator), "tmp", "cohere-test-1"),
			isolated: true},
		{name: "the person's system cache", systemCache: filepath.Join(usersCache, "cohere")},
		{name: "inside the person's system cache", systemCache: filepath.Join(usersCache, "cohere", "tests")},
		{name: "holding the person's system cache", systemCache: usersCache},
		{name: "relative", systemCache: "cohere-test"},
		{name: "unset", systemCache: ""},
	} {
		if problem := isolationProblem(each.systemCache, usersCache); (problem == "") != each.isolated {
			test.Errorf("%s: isolated %t, want %t (%q)", each.name, problem == "", each.isolated, problem)
		}
	}
	if problem := isolationProblem(filepath.Join(usersCache, "cohere"), ""); problem != "" {
		test.Errorf("with no user cache directory, want nothing to guard, got %q", problem)
	}
}
