package dispatch

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/release/packaging"
)

// The development engine is cached by a hash, and it reports whether the tree it was built from had
// uncommitted changes. A hash that leaves that state out serves an engine built in one state to a tree in
// the other (#50wtb2s): a clean tree was refused at land for an engine built while an unrelated file was
// dirty, and a dirty tree could be told its build was clean. These tests resolve the development engine
// with the fixture's stand-in for SwiftPM, which reports the state it was handed, and read what each
// engine says about itself.

// testSwiftVersionOutput is what `swift --version` prints, as the resolver is handed it.
const testSwiftVersionOutput = "Apple Swift version 6.4 (" + testToolchain + ")\nTarget: arm64-apple-macosx26.0\n"

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in.
func TestAnEngineBuiltFromADirtyTreeIsNotServedOnceTheTreeIsClean(test *testing.T) {
	fixture := newSwiftFixture(test)
	notes := filepath.Join(fixture.repository, "notes.txt")
	writeFile(test, notes, "a file nobody committed, nowhere near the engine\n")

	dirty, built := fixture.resolveDevelopment(test)
	if !built || !engineSaysModified(test, dirty) {
		test.Fatalf("an engine built from a dirty tree says it is clean (built=%v)", built)
	}

	if removeError := os.Remove(notes); removeError != nil {
		test.Fatal(removeError)
	}
	clean, built := fixture.resolveDevelopment(test)
	if engineSaysModified(test, clean) {
		test.Fatalf(
			"the clean tree was served an engine that says its tree was modified (built=%v, the same binary: %v)",
			built,
			clean == dirty,
		)
	}
	if !built || clean == dirty {
		test.Fatalf("the clean tree was served the dirty tree's engine (built=%v)", built)
	}

	// Each state keeps its own engine, so going back is a cache hit, not a rebuild.
	writeFile(test, notes, "back again\n")
	again, built := fixture.resolveDevelopment(test)
	if built || again != dirty {
		test.Fatalf("the dirty state did not find its own engine again (built=%v)", built)
	}
	fixture.expectEveryBuildWasTold(test)
}

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in.
func TestAnEngineBuiltFromACleanTreeIsNotServedOnceTheTreeIsDirty(test *testing.T) {
	fixture := newSwiftFixture(test)

	clean, built := fixture.resolveDevelopment(test)
	if !built || engineSaysModified(test, clean) {
		test.Fatalf("an engine built from a clean tree says it was modified (built=%v)", built)
	}

	// A tracked file outside the engine's sources, so nothing the engine hash reads has moved.
	writeFile(test, filepath.Join(fixture.repository, ".gitignore"), "/.cache/\n/scratch/\n")
	dirty, built := fixture.resolveDevelopment(test)
	if !engineSaysModified(test, dirty) {
		test.Fatalf(
			"the dirty tree was served an engine that claims a clean build (built=%v, the same binary: %v)",
			built,
			dirty == clean,
		)
	}
	if !built || dirty == clean {
		test.Fatalf("the dirty tree was served the clean tree's engine (built=%v)", built)
	}
	fixture.expectEveryBuildWasTold(test)
}

// The build SwiftPM runs is handed the state the caller computed and nothing else: one this process
// inherited is taken out, so a committed build, which is handed none, reads git.
//
// Not parallel: it puts a fake swift first on PATH, and sets the variable in this process, with test.Setenv.
func TestTheSwiftBuildIsHandedOnlyTheStatedTreeState(test *testing.T) {
	bin := test.TempDir()
	product := test.TempDir()
	recorded := filepath.Join(test.TempDir(), "stated")
	writeFile(test, filepath.Join(product, "cohere-swift"), "#!/bin/sh\n")
	fake := "#!/bin/sh\ncase \"$*\" in\n*--show-bin-path*) echo " + product + " ;;\n" +
		"*) printf '%s' \"${" + release.SwiftSourceTreeModifiedVariable + "-unset}\" > " + recorded + " ;;\nesac\n"
	writeFile(test, filepath.Join(bin, "swift"), fake)
	if chmodError := os.Chmod(filepath.Join(bin, "swift"), 0o755); chmodError != nil {
		test.Fatal(chmodError)
	}
	test.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	test.Setenv(release.SwiftSourceTreeModifiedVariable, "true")

	stated := func(environment []string) string {
		test.Helper()
		if buildError := buildSwiftProductWithSwiftPm(
			test.TempDir(),
			test.TempDir(),
			filepath.Join(test.TempDir(), "bin", "cohere-swift"),
			environment,
		); buildError != nil {
			test.Fatal(buildError)
		}
		return readFile(test, recorded)
	}

	if got := stated([]string{release.SwiftSourceTreeModifiedVariable + "=false"}); got != "false" {
		test.Errorf("a build handed a clean tree was told %q", got)
	}
	if got := stated(nil); got != "unset" {
		test.Errorf("a build handed no state was told %q, inherited from this process", got)
	}
}

// The engine's real manifest takes the stated answer over git's, both ways, and reads git when nothing is
// stated. The stand-in package's manifest is the real one's `sourceTreeModified`, copied from it here, so
// the expression under test is the one the engine builds with.
func TestTheEngineManifestPrefersTheStatedTreeState(test *testing.T) {
	test.Parallel()
	if runtime.GOOS != "darwin" {
		test.Skip("the Swift engine is built only on macOS")
	}
	if _, lookPathError := exec.LookPath("swift"); lookPathError != nil {
		test.Skip("no swift toolchain on PATH")
	}

	manifest := readFile(test, filepath.Join("..", "..", "..", "swift", "Package.swift"))
	start := strings.Index(manifest, "let sourceTreeModified =")
	end := strings.Index(manifest[max(start, 0):], "\n    }\n")
	if start < 0 || end < 0 {
		test.Fatal("swift/Package.swift holds no `let sourceTreeModified =` closed by `    }`, so this test reads nothing")
	}
	expression := manifest[start : start+end+len("\n    }\n")]

	module := test.TempDir()
	packageDirectory := filepath.Join(module, "swift")
	writeFile(test, filepath.Join(packageDirectory, "Package.swift"), "// swift-tools-version:6.2\n"+
		"import PackageDescription\n"+expression+
		"let package = Package(name: \"Stand\", products: [.executable(name: \"cohere-swift\", targets: [\"Stand\"])],\n"+
		"    targets: [.executableTarget(name: \"Stand\", swiftSettings: sourceTreeModified ? [.define(\"TREE_MODIFIED\")] : [])])\n")
	writeFile(test, filepath.Join(packageDirectory, "Sources", "Stand", "main.swift"),
		"#if TREE_MODIFIED\nprint(\"modified\")\n#else\nprint(\"clean\")\n#endif\n")
	writeFile(test, filepath.Join(module, ".gitignore"), ".scratch/\n")
	gitCommand(test, module, "init", "-q")
	gitCommand(test, module, "add", ".")
	gitCommand(test, module, "commit", "-q", "-m", "stand-in engine")

	scratch := filepath.Join(module, ".scratch")
	says := func(environment []string) string {
		test.Helper()
		binaryPath := filepath.Join(test.TempDir(), "cohere-swift")
		if buildError := buildSwiftProductWithSwiftPm(packageDirectory, scratch, binaryPath, environment); buildError != nil {
			test.Fatal(buildError)
		}
		return strings.TrimSpace(run(test, binaryPath))
	}
	stating := func(modified string) []string {
		return []string{release.SwiftSourceTreeModifiedVariable + "=" + modified}
	}

	if got := says(nil); got != "clean" {
		test.Fatalf("a clean tree with nothing stated built an engine that says %q", got)
	}
	if got := says(stating("true")); got != "modified" {
		test.Errorf("a clean tree stated modified built an engine that says %q", got)
	}
	writeFile(test, filepath.Join(module, "notes.txt"), "uncommitted\n")
	if got := says(stating("false")); got != "clean" {
		test.Errorf("a dirty tree stated clean built an engine that says %q", got)
	}
	if got := says(nil); got != "modified" {
		test.Errorf("a dirty tree with nothing stated built an engine that says %q", got)
	}
}

// A worktree whose TypeScript is a symlink to another checkout's, as most of the house's are, is one git
// status refuses unless submodules are left out. Its state still reads: clean when clean, modified when a
// file nobody committed is in it, and never for an ignored one.
func TestTheTreeStateReadsInAWorktreeWhoseSubmoduleIsASymlink(test *testing.T) {
	test.Parallel()
	fixture := newCommittedFixture(test)
	submodule := filepath.Join(fixture.superproject, "TypeScript")
	elsewhere := filepath.Join(test.TempDir(), "TypeScript")
	if renameError := os.Rename(submodule, elsewhere); renameError != nil {
		test.Fatal(renameError)
	}
	if symlinkError := os.Symlink(elsewhere, submodule); symlinkError != nil {
		test.Fatal(symlinkError)
	}

	if workingTreeModified(fixture.superproject) {
		test.Fatal("a clean worktree whose submodule is a symlink reads as modified")
	}
	writeFile(test, filepath.Join(fixture.superproject, "ignored.log"), "ignored\n")
	writeFile(test, filepath.Join(fixture.superproject, ".git", "info", "exclude"), "ignored.log\n")
	if workingTreeModified(fixture.superproject) {
		test.Fatal("an ignored file made the tree read as modified")
	}
	writeFile(test, filepath.Join(fixture.superproject, "notes.txt"), "uncommitted\n")
	if !workingTreeModified(fixture.superproject) {
		test.Fatal("an untracked file left the tree reading clean")
	}
}

// resolveDevelopment resolves the engine from the fixture's working tree, failing the test on error.
func (fixture *swiftFixture) resolveDevelopment(test *testing.T) (string, bool) {
	test.Helper()
	binaryPath, built, resolveSwiftEngineError := ResolveSwiftEngine(fixture.paths, "", 2, testSwiftVersionOutput)
	if resolveSwiftEngineError != nil {
		test.Fatalf("resolving the development engine: %v", resolveSwiftEngineError)
	}
	return binaryPath, built
}

// expectEveryBuildWasTold fails the test if a development build was left to read git for itself, a moment
// apart from the answer its hash holds.
func (fixture *swiftFixture) expectEveryBuildWasTold(test *testing.T) {
	test.Helper()
	for index, stated := range fixture.stated {
		if stated != "true" && stated != "false" {
			test.Errorf("development build %d was handed %q for the tree's state", index+1, stated)
		}
	}
}

// engineSaysModified is the sourceTreeModified an engine's provenance record reports.
func engineSaysModified(test *testing.T, binaryPath string) bool {
	test.Helper()
	var record struct {
		Kind               string `json:"kind"`
		SourceTreeModified *bool  `json:"sourceTreeModified"`
	}
	output := run(test, binaryPath, "--contract", "2", "--version")
	if json.Unmarshal([]byte(strings.TrimSpace(output)), &record) != nil || record.Kind != "provenance" ||
		record.SourceTreeModified == nil {
		test.Fatalf("%s printed no provenance record naming sourceTreeModified: %q", binaryPath, output)
	}
	return *record.SourceTreeModified
}
