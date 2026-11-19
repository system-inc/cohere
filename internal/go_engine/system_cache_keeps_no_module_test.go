package go_engine

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// thirdPartyModulePath is the import path of the third-party module writeThirdPartyModuleProject's project requires.
const thirdPartyModulePath = "example.com/dependency"

// writeThirdPartyModuleProject writes a project that requires a third-party module, example.com/dependency, and,
// below a directory of the test's own that every go command of the run is told is the module cache, the module, which
// a replace points at, so go list names its files below the module cache in a module that is not a main one, as it
// names a module it downloaded, with no network and no real module cache. The module reads strings, so the standard
// library is reached through it. It returns the project's root.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func writeThirdPartyModuleProject(test *testing.T) string {
	test.Helper()
	// Resolved, so the module cache go env reports and the replace's directory name the files as the root's resolved
	// path does, the engine's root being resolved (resolveRoot).
	moduleCache, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	dependency := filepath.Join(moduleCache, "example.com", "dependency@v1.0.0")
	writeFixtureFiles(test, dependency, map[string]string{
		"go.mod": "module example.com/dependency\n\ngo 1.21\n",
		"dependency.go": "// Package dependency is a third-party module the project requires.\npackage dependency\n\n" +
			"import \"strings\"\n\n// Trimmed reads the standard library.\n" +
			"var Trimmed = strings.TrimSpace(\" trimmed \")\n",
	})
	project, resolveError := filepath.EvalSymlinks(test.TempDir())
	if resolveError != nil {
		test.Fatal(resolveError)
	}
	writeFixtureFiles(test, project, map[string]string{
		"go.mod": "module example.com/project\n\ngo 1.21\n\nrequire example.com/dependency v1.0.0\n\n" +
			"replace example.com/dependency => " + filepath.ToSlash(dependency) + "\n",
		"project.go": "// Package project reads the dependency.\npackage project\n\n" +
			"import \"example.com/dependency\"\n\n// Length is the dependency's value's length.\n" +
			"var Length = len(dependency.Trimmed)\n",
	})
	test.Setenv("GOMODCACHE", moduleCache)
	test.Setenv("GOPROXY", "off")
	test.Setenv("GOSUMDB", "off")
	test.Setenv("GOTOOLCHAIN", "local")
	test.Setenv("GOWORK", "off")
	// The cache's files are written read-only otherwise, should go write any, and the test's cleanup could not
	// remove them.
	test.Setenv("GOFLAGS", strings.TrimSpace(os.Getenv("GOFLAGS")+" -modcacherw"))
	return project
}

// filesNaming is every file below root whose bytes hold text, by path.
func filesNaming(test *testing.T, root string, text string) []string {
	test.Helper()
	var naming []string
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		if bytes.Contains(contents, []byte(text)) {
			naming = append(naming, path)
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return naming
}

// The system cache keeps the standard library alone: a third-party module's package, every file of it in the module
// cache, is never kept there nor restored from it. Two runs under `--no-cache` over one system cache write the
// standard library the first time and restore all of it the second, every entry the first wrote a standard-library
// package's, so the dependency is checked from source both times, and no file of the system cache names it.
// Not parallel: it sets the go environment of the process, which every go command of the run in it reads
func TestTheSystemCacheKeepsNoThirdPartyModule(test *testing.T) {
	root := writeThirdPartyModuleProject(test)
	source := standardLibrarySource{stamp: engineTestsStamp, systemCacheRoot: test.TempDir()}
	first := runEngineWith(test, root, source, rules.All(), "--no-cache")
	second := runEngineWith(test, root, source, rules.All(), "--no-cache")
	if first.counts.SystemCacheWritten == 0 || second.counts.SystemCacheWritten != 0 ||
		second.counts.FromSystemCache != first.counts.SystemCacheWritten {
		test.Errorf("want every entry the first run wrote a standard-library package's the second restored, and "+
			"nothing of the dependency written or restored, first %+v, second %+v", first.counts, second.counts)
	}
	if naming := filesNaming(test, source.systemCacheRoot, thirdPartyModulePath); len(naming) != 0 {
		test.Errorf("the system cache holds %s in %s", thirdPartyModulePath, strings.Join(naming, ", "))
	}
	if second.withoutCounts != first.withoutCounts {
		test.Errorf("second\n%s\nfirst\n%s", second.withoutCounts, first.withoutCounts)
	}
}
