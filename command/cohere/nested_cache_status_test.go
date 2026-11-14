package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// A run gives a library whose own rules say nothing of `.cache` its cache, and leaves both the library's git status
// and the root's as they were (#f6y78ws): the directory is made inside another repository's checkout, so the
// library's own status is the one that says whether it was left dirty, and the root sees the library as a submodule
// whose content changed when it was. A file in the cache is ignored with it, and the control takes the library's
// ignore file away, where both show it.
func TestALibrarysCacheLeavesBothRepositoriesClean(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	root, library := libraryFixture(test)
	status := func(repository string) string {
		test.Helper()
		return gitInRepository(test, repository, "status", "--porcelain", "--untracked-files=all")
	}
	if before := status(root) + status(library); before != "" {
		test.Fatalf("the fixture is not clean before the run:\n%s", before)
	}

	// The fixtures' JSON is not in the house format, and formatting is not this test's subject.
	output, exitCode := runCohereWithEnvironment(test, binary, root, nil, "--no-fix", "--no-format")
	if exitCode != 0 {
		test.Fatalf("exit %d from a clean fixture:\n%s", exitCode, output)
	}
	ignoreFile := filepath.Join(cache_directory.Of(library), ".gitignore")
	if contents, readError := os.ReadFile(ignoreFile); readError != nil ||
		string(contents) != cache_directory.IgnoreFileContents {
		test.Fatalf("the library got no cache ignoring itself: %q (%v)\n%s", contents, readError, output)
	}
	for _, repository := range []string{library, root} {
		if after := status(repository); after != "" {
			test.Errorf("the run left %s dirty:\n%s\n--- the run\n%s", repository, after, output)
		}
	}

	// A file in the cache, as the library's format record is written there by a check that formats. Ignored with it,
	// and without the ignore file what the control shows, since git never shows an empty directory.
	if writeError := os.WriteFile(
		filepath.Join(cache_directory.Of(library), "format.gob"),
		[]byte("a record"),
		0o644,
	); writeError != nil {
		test.Fatal(writeError)
	}
	for _, repository := range []string{library, root} {
		if after := status(repository); after != "" {
			test.Errorf("a file in the library's cache left %s dirty:\n%s", repository, after)
		}
	}
	if removeError := os.Remove(ignoreFile); removeError != nil {
		test.Fatal(removeError)
	}
	if after := status(library); !strings.Contains(after, ".cache/") {
		test.Errorf("without the ignore file the library's status does not show the cache, so it proves nothing:\n%s",
			after)
	}
	if after := status(root); !strings.Contains(after, "library") {
		test.Errorf("without the ignore file the root's status does not show the library, so it proves nothing:\n%s",
			after)
	}
}

// libraryFixture is a committed root holding a TypeScript project and a library as a committed submodule, both
// repositories clean, the library's own rules saying nothing of `.cache`. It returns the root and the library.
func libraryFixture(test *testing.T) (string, string) {
	test.Helper()
	root := test.TempDir()
	library := filepath.Join(root, "library")
	writeTree(test, library, map[string]string{"Library.ts": "export const library = 1;\n"})
	commit := func(repository string) {
		test.Helper()
		gitInRepository(test, repository, "-c", "user.name=cohere", "-c", "user.email=cohere@example.com",
			"commit", "--quiet", "-m", "fixture")
	}
	gitInRepository(test, library, "init", "--quiet")
	gitInRepository(test, library, "add", "--all")
	commit(library)
	libraryCommit := strings.TrimSpace(gitInRepository(test, library, "rev-parse", "HEAD"))

	writeTree(test, root, map[string]string{
		"tsconfig.json":       incrementalFixtureConfiguration,
		"CohereSettings.json": "{ \"rules\": {} }\n",
		"index.ts":            "export const value = 1;\n",
		".gitmodules":         "[submodule \"library\"]\n\tpath = library\n\turl = ../library\n",
	})
	gitInRepository(test, root, "init", "--quiet")
	gitInRepository(test, root, "add", "tsconfig.json", "CohereSettings.json", "index.ts", ".gitmodules")
	gitInRepository(test, root, "update-index", "--add", "--cacheinfo", "160000,"+libraryCommit+",library")
	commit(root)
	return root, library
}

// A run that only answers, `--version`, a rule list, the resolved configuration or the cache dump, checks nothing,
// so it makes no cache in any library (#spdcohere). `cohere --version` made each library's `.cache/cohere`, and the
// quiet bench, which runs it in its copy to pin the engine, read the copy's tree as changed under its runs and lost
// the night. A checking run still makes it, which TestALibrarysCacheLeavesBothRepositoriesClean holds.
func TestARunThatOnlyAnswersMakesNoCacheInALibrary(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, flag := range answerOnlyFlags {
		test.Run(flag, func(test *testing.T) {
			test.Parallel()
			root, library := libraryFixture(test)
			output, _ := runCohereWithEnvironment(test, binary, root, nil, "--"+flag)
			for _, repository := range []string{library, root} {
				if _, statError := os.Stat(filepath.Join(repository, ".cache")); statError == nil {
					test.Errorf("cohere --%s made %s's .cache:\n%s", flag, repository, output)
				}
			}
		})
	}
}
