//go:build unix

// The stand-in cohere-swift here is a /bin/sh script, as combined_footer_unix_test.go's is, so the file builds on Unix.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/executable_testing"
)

// Before any project's run starts, the cache directory of each project whose run keeps a run cache is made, and of no
// other (#d46ks7h): not a TypeScript program run with the cache off because it shares its directory, and not a run
// whose arguments no run cache admits.
func TestEveryProjectRunThatKeepsACacheHasItsDirectoryBeforeAnyRunStarts(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	launches := []projectLaunch{
		{project: discoveredProject{Directory: ".", Kind: projectKindGoModule}, arguments: []string{"--no-fix"}},
		{project: discoveredProject{Directory: "swift", Kind: projectKindSwiftPackage}, arguments: []string{"--no-fix"}},
		{project: discoveredProject{Directory: "web", Kind: projectKindTypeScriptProgram}, arguments: []string{"--no-cache"}},
		{project: discoveredProject{Directory: "timed", Kind: projectKindGoModule}, arguments: []string{"--timing"}},
	}
	for _, launch := range launches {
		if makeError := os.MkdirAll(filepath.Join(root, launch.project.Directory), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
	}
	makeProjectCacheDirectories(root, launches)
	for directory, want := range map[string]bool{".": true, "swift": true, "web": false, "timed": false} {
		_, statError := os.Stat(cache_directory.Of(filepath.Join(root, directory)))
		if made := statError == nil; made != want {
			test.Errorf("%s's cache directory made %t, want %t", directory, made, want)
		}
	}
}

// A fresh tree holding a Go module beside a Swift package records its Go side on its first plain run, so the second
// replays it (#d46ks7h). Before, the Swift project's run made swift/.cache while the Go census read that directory,
// and the Go run refused to record a run whose input moved under it, so the second run was cold too, in about one
// first run in six here. Twelve fresh trees, so the old order shows in most runs of this test without the fix.
func TestAFreshTreesSecondRunReplaysItsGoSide(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	fixture, absoluteError := filepath.Abs(filepath.Join("..", "..", "swift", "Contract", "Clean.jsonl"))
	if absoluteError != nil {
		test.Fatal(absoluteError)
	}
	swiftEngine := filepath.Join(test.TempDir(), "cohere-swift")
	if writeError := executable_testing.WriteFile(swiftEngine, []byte("#!/bin/sh\ncat '"+fixture+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	environment := []string{"COHERE_SWIFT_ENGINE=" + swiftEngine, goChildVariable + "="}
	for tree := range 12 {
		test.Run(fmt.Sprintf("tree %d", tree), func(test *testing.T) {
			test.Parallel()
			root, resolveError := filepath.EvalSymlinks(test.TempDir())
			if resolveError != nil {
				test.Fatalf("resolving the temporary directory: %v", resolveError)
			}
			writeTree(test, root, map[string]string{
				".gitignore":          ".cache/\n",
				goModuleMarker:        "module example.com/fresh\n\ngo 1.27\n",
				"alpha/alpha.go":      "// Package alpha is one.\npackage alpha\n\n// MaxSize is a size.\nconst MaxSize = 1\n",
				"swift/Package.swift": "// swift-tools-version: 6.0\n",
			})
			initialize := exec.Command("git", "init", "--quiet")
			initialize.Dir = root
			if initializeOutput, initializeError := initialize.CombinedOutput(); initializeError != nil {
				test.Fatalf("git init: %v\n%s", initializeError, initializeOutput)
			}
			first, firstExit := runCohereWithEnvironment(test, binary, root, environment, "--no-fix", "--verbose")
			if firstExit != 0 {
				test.Fatalf("the first run exited %d:\n%s", firstExit, first)
			}
			second, secondExit := runCohereWithEnvironment(test, binary, root, environment, "--no-fix", "--verbose")
			goSection := second[strings.Index(second, "== . (Go) =="):]
			goSection = goSection[:strings.Index(goSection, "\n\n")]
			if secondExit != 0 || !strings.Contains(goSection, "• replayed") {
				test.Errorf("the second run exited %d, want 0 and its Go side replayed:\n%s\nthe first run:\n%s",
					secondExit, second, first)
			}
		})
	}
}
