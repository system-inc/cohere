package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTheDevelopmentHashLandsInCoheresOwnCache pins where the launcher's one plain-file write goes.
//
// `cohere --no-fix` promises to write nothing to the project it checks, and the help text states the
// boundary of that promise as cohere's own `.cache/cohere/`. That boundary is only true while every
// launcher write is placed by Paths rather than by the working directory, so this runs the write from
// inside a stand-in project and asserts two things: the hash is under the module's cache, and the
// project directory is still empty.
//
// A path built relative to the working directory, which is the easy mistake when a file sits "beside"
// a binary, would put it in whatever tree the caller was standing in. That would make the help text
// false while every other launcher test kept passing.
//
// Not parallel: it moves into a stand-in project with test.Chdir, which a parallel test may not call.
func TestTheDevelopmentHashLandsInCoheresOwnCache(test *testing.T) {
	module := test.TempDir()
	project := test.TempDir()
	paths := DefaultPaths(module)
	if createDirectoryError := os.MkdirAll(paths.BinaryDirectory(), 0o755); createDirectoryError != nil {
		test.Fatal(createDirectoryError)
	}

	test.Chdir(project)
	if recordDevelopmentHashError := recordDevelopmentHash(paths, "0123456789abcdef"); recordDevelopmentHashError != nil {
		test.Fatal(recordDevelopmentHashError)
	}

	cache := filepath.Join(module, ".cache", "cohere") + string(filepath.Separator)
	written := paths.developmentHashPath()
	if !filepath.IsAbs(written) || !strings.HasPrefix(written, cache) {
		test.Errorf("the development hash path %s is not inside the module's cache %s", written, cache)
	}
	if contents, readError := os.ReadFile(written); readError != nil || strings.TrimSpace(string(contents)) != "0123456789abcdef" {
		test.Errorf("the hash was not written where the path says (read %q, %v)", contents, readError)
	}

	entries, readDirectoryError := os.ReadDir(project)
	if readDirectoryError != nil {
		test.Fatal(readDirectoryError)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		test.Errorf("recording the development hash wrote into the directory it ran from: %v", names)
	}
}
