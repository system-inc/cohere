package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
)

// gitInRepository runs git in repository with git's global and system configuration read from nowhere, so no
// person's excludes file decides what the test sees, and returns its output.
func gitInRepository(test *testing.T, repository string, arguments ...string) string {
	test.Helper()
	command := exec.Command("git", arguments...)
	command.Dir = repository
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	output, runError := command.CombinedOutput()
	if runError != nil {
		test.Fatalf("git %s: %v\n%s", strings.Join(arguments, " "), runError, output)
	}
	return string(output)
}

// A cold cache-on run in a repository whose rules say nothing of `.cache/` leaves git status as it found it (#6vhp0vd):
// the run that creates `.cache/cohere` writes an ignore file in it holding `*`, at a TypeScript root and at a Go root,
// whose engine writes its own store there too. Each run is shown to have kept a cache, so the clean status is not a
// run that wrote nothing, and the control takes the ignore file away, where git shows the cache.
func TestAColdRunLeavesNoUntrackedCache(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	for _, fixture := range []struct {
		name  string
		files map[string]string
		// kept is a path under the cache directory the run must have written.
		kept string
	}{
		{
			name: "TypeScript",
			files: map[string]string{
				"tsconfig.json":       incrementalFixtureConfiguration,
				"CohereSettings.json": "{ \"rules\": {} }\n",
				"index.ts":            "export const value = 1;\n",
			},
			kept: "findings.gob",
		},
		{
			name: "Go",
			files: map[string]string{
				goModuleMarker:   "module example.com/cached\n\ngo 1.27\n",
				"alpha/alpha.go": "// Package alpha is one.\npackage alpha\n\n// Size is one.\nconst Size = 1\n",
			},
			kept: "go",
		},
	} {
		test.Run(fixture.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			writeTree(test, root, fixture.files)
			gitInRepository(test, root, "init", "--quiet")
			gitInRepository(test, root, "add", "--all")
			gitInRepository(test, root, "-c", "user.name=cohere", "-c", "user.email=cohere@example.com",
				"commit", "--quiet", "-m", "fixture")
			// The child mark is emptied, since a front door that inherited it would refuse to start the Go engine. The
			// fixtures' JSON is not in the house format, and formatting is not this test's subject.
			output, exitCode := runCohereWithEnvironment(test, binary, root, []string{goEngineChildVariable + "="},
				"--no-fix", "--no-format")
			if exitCode != 0 {
				test.Fatalf("exit %d from a clean fixture:\n%s", exitCode, output)
			}
			if _, statError := os.Stat(filepath.Join(cache_directory.Of(root), fixture.kept)); statError != nil {
				test.Fatalf("the run kept no %s in its cache, so a clean status proves nothing (%v):\n%s",
					fixture.kept, statError, output)
			}
			if status := gitInRepository(test, root, "status", "--porcelain", "--untracked-files=all"); status != "" {
				test.Errorf("a cold run left git status dirty:\n%s\n--- the run\n%s", status, output)
			}
			if strings.Contains(output, "does not ignore .cache/") {
				test.Errorf("a cache that ignores itself was said to be tracked:\n%s", output)
			}

			if removeError := os.Remove(filepath.Join(cache_directory.Of(root), ".gitignore")); removeError != nil {
				test.Fatal(removeError)
			}
			if status := gitInRepository(test, root, "status", "--porcelain"); !strings.Contains(status, ".cache/") {
				test.Errorf(
					"without the ignore file git does not show the cache, so the test proves nothing:\n%s",
					status,
				)
			}
		})
	}
}
