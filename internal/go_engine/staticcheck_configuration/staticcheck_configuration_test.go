package staticcheck_configuration

import (
	"path/filepath"
	"testing"
)

// Directory skips a file inside a build cache and nothing else: a file in a directory whose name only begins with the
// cache's is a package's own, which upstream's bare prefix test took for the cache's, and a `GOCACHE` of "off", as go
// env reports a cache turned off, makes no directory a build cache.
func TestDirectorySkipsOnlyFilesInsideABuildCache(test *testing.T) {
	test.Parallel()
	root, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	buildCache := filepath.Join(root, "cache")
	AddBuildCache(buildCache)
	AddBuildCache("off")

	beside := filepath.Join(root, "cache-beside", "beside.go")
	if directory := Directory([]string{beside}); directory != filepath.Dir(beside) {
		test.Errorf("a package beside the build cache reads its staticcheck.conf from %q, want %q", directory,
			filepath.Dir(beside))
	}
	cached := filepath.Join(buildCache, "4f", "4f00c0ffee-d")
	if directory := Directory([]string{cached}); directory != "" {
		test.Errorf("a package of a build-cache file alone reads its staticcheck.conf from %q, want none", directory)
	}
	relative := filepath.Join("off", "relative.go")
	if directory := Directory([]string{relative}); directory != "off" {
		test.Errorf("a file under a directory named off reads its staticcheck.conf from %q, want off", directory)
	}
}
