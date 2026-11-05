//go:build unix

// Every project here is checked through a stand-in Swift engine, a /bin/sh script, so the file builds on
// Unix, as projects_test.go does (#tejf9bc).

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The `swift` block in CohereSettings.json is the Swift engine's, and one settings file configures every
// engine: the TypeScript loader leaves the block to that engine and refuses one no Swift run reads.

const swiftOnlySettings = "{ \"swift\": { \"rules\": { \"cohere-swift/file-length\": [\"error\", { \"max\": 1 }] } } }\n"

// loggingSwiftEngine is a clean stand-in engine that appends each command line it is given to the file it
// returns.
func loggingSwiftEngine(test *testing.T) (string, string) {
	test.Helper()
	log := filepath.Join(test.TempDir(), "arguments")
	return fakeSwiftEngine(test, "echo \"$@\" >> '"+log+"'\ncat "+filepath.Join(contractDirectory(test), "Clean.jsonl")), log
}

func readLog(test *testing.T, log string) string {
	test.Helper()
	contents, readError := os.ReadFile(log)
	if readError != nil && !os.IsNotExist(readError) {
		test.Fatal(readError)
	}
	return string(contents)
}

// The reproduction: a Swift package whose settings hold only a swift block was refused before anything ran,
// because discovery's read of the root's ignorePatterns went through a loader that knew no "swift". It runs,
// and the engine is handed the package.
func TestASwiftPackageWhoseSettingsHoldOnlyASwiftBlockRuns(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	engine, log := loggingSwiftEngine(test)
	root := newRepository(test, map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
		"CohereSettings.json":   swiftOnlySettings,
	})
	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 || strings.Contains(output, "does not implement") {
		test.Fatalf("a package whose settings hold only a swift block did not run (exit %d):\n%s", exitCode, output)
	}
	if arguments := readLog(test, log); !strings.Contains(arguments, "--root ") || !strings.Contains(arguments, filepath.Base(root)) {
		test.Fatalf("the engine was not handed the package; it was given:\n%s\noutput:\n%s", arguments, output)
	}
}

// A directory holding a tsconfig and a Package.swift that share one settings file is two projects, and the
// file's swift block stops neither: the TypeScript run loads it, and the Swift run is handed the directory.
func TestATypeScriptProgramAndASwiftPackageShareOneSettingsFile(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	engine, log := loggingSwiftEngine(test)
	root := mixedRepository(test, ".")
	writeTree(test, root, map[string]string{
		"CohereSettings.json": "{ \"rules\": {}, \"swift\": { \"rules\": { \"cohere-swift/file-length\": \"error\" } } }\n",
	})
	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix")
	if exitCode != 0 || !strings.Contains(output, "== . (TypeScript) ==") || !strings.Contains(output, "== . (Swift) ==") {
		test.Fatalf("a shared settings file with a swift block did not let both engines run (exit %d):\n%s", exitCode, output)
	}
	if !strings.Contains(readLog(test, log), "--root ") {
		test.Fatalf("the engine was not handed the package:\n%s", output)
	}
}

// A swift block no Swift run reads is refused by name: beside only a tsconfig, on discovery's read from the
// root and on the TypeScript run's own when a directory is named and nothing is discovered; at a root above
// the package; and in a base the package's file extends, which the engine never follows, though the package
// beside it is a Swift one.
func TestASwiftBlockNoSwiftRunReadsIsRefusedByName(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	engine, log := loggingSwiftEngine(test)

	typeScriptOnly := newRepository(test, map[string]string{
		"tsconfig.json":       incrementalFixtureConfig + "\n",
		"CohereSettings.json": "{ \"rules\": {}, \"swift\": { \"rules\": {} } }\n",
		"index.ts":            "export const value = 1;\n",
		".gitignore":          "tsconfig.tsbuildinfo\n",
	})
	for _, arguments := range [][]string{{"--no-fix"}, {"--no-fix", "--directory", typeScriptOnly}} {
		output, exitCode := runWithEngine(test, binary, engine, typeScriptOnly, arguments...)
		if exitCode == 0 || !strings.Contains(output, `CohereSettings.json declares "swift"`) || !strings.Contains(output, "holds no Swift project") {
			test.Errorf("a swift block beside only a tsconfig was not refused by name, run with %v (exit %d):\n%s", arguments, exitCode, output)
		}
	}

	// The block at the repository's root, the package below it: the package's run reads the settings beside
	// its own Package.swift, so nothing reads the root's. Only discovery reads that file this side of the engine.
	packageBelow := newRepository(test, map[string]string{"CohereSettings.json": swiftOnlySettings})
	writeTree(test, filepath.Join(packageBelow, "apple", "Toy"), map[string]string{"Package.swift": discoveryPackage, "Sources/Toy/Toy.swift": "let toy = 1\n"})
	output, exitCode := runWithEngine(test, binary, engine, packageBelow, "--no-fix")
	if exitCode == 0 || !strings.Contains(output, `CohereSettings.json declares "swift"`) || !strings.Contains(output, "holds no Swift project") {
		test.Errorf("a swift block at the root above the package was not refused by name (exit %d):\n%s", exitCode, output)
	}

	inBase := newRepository(test, map[string]string{
		"Package.swift":         discoveryPackage,
		"Sources/Toy/Toy.swift": "let toy = 1\n",
		"Base.json":             swiftOnlySettings,
		"CohereSettings.json":   "{ \"extends\": \"./Base.json\" }\n",
	})
	output, exitCode = runWithEngine(test, binary, engine, inBase, "--no-fix")
	if exitCode == 0 || !strings.Contains(output, `Base.json declares "swift", and it is extended by`) {
		test.Errorf("a swift block in an extended base was not refused by name (exit %d):\n%s", exitCode, output)
	}
	if arguments := readLog(test, log); arguments != "" {
		test.Errorf("the engine ran on a refused settings file:\n%s", arguments)
	}
}

// A settings file the command line names is the Swift engine's to read wherever it sits, so its swift block
// is not refused for sitting beside no Swift project: every project's run is handed it, the Swift one too.
func TestANamedSettingsFileAwayFromTheSwiftProjectIsNotRefused(test *testing.T) {
	test.Parallel()
	binary := buildCohere(test)
	engine, log := loggingSwiftEngine(test)
	root := mixedRepository(test, "apple/Toy")
	writeTree(test, filepath.Join(root, "settings"), map[string]string{
		"Shared.json": "{ \"rules\": {}, \"swift\": { \"rules\": { \"cohere-swift/file-length\": \"error\" } } }\n",
	})
	output, exitCode := runWithEngine(test, binary, engine, root, "--no-fix", "--lint-config", filepath.Join(root, "settings", "Shared.json"))
	if exitCode != 0 || !strings.Contains(output, "apple/Toy (Swift): green") {
		test.Fatalf("a named settings file with a swift block was refused (exit %d):\n%s", exitCode, output)
	}
	if arguments := readLog(test, log); !strings.Contains(arguments, "--lint-config "+filepath.Join(root, "settings", "Shared.json")) {
		test.Fatalf("the engine was not handed the named file; it was given:\n%s", arguments)
	}
}
