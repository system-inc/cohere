package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Swift engine a gate runs must come from the commit the gate's cohere was built from. These tests
// stand a script in for SwiftPM, because a real cold build takes minutes and what is under test is
// which tree gets built and when. The script records the tree it was handed, and reports
// sourceTreeModified the way the real manifest does, from git's view of the package it was built in.

const testToolchain = "swiftlang-6.4.0.34.1"

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and two
// fixtures at once would build with each other's.
func TestTheCommittedSwiftEngineIsBuiltFromTheCommitNotTheWorkingTree(test *testing.T) {
	fixture := newSwiftFixture(test)

	first, built, err := resolveCommittedSwiftEngine(fixture.paths, fixture.head(test), testToolchain, 2)
	if err != nil {
		test.Fatalf("building from the first commit: %v", err)
	}
	if !built {
		test.Fatal("an empty cache reported no build")
	}
	if output := run(test, first); output != "rule=one\n" {
		test.Fatalf("the first engine printed %q", output)
	}

	// An uncommitted edit in the checkout is not part of the commit, so the same engine serves.
	writeFile(test, fixture.rule(), "one, uncommitted\n")
	again, built, err := resolveCommittedSwiftEngine(fixture.paths, fixture.head(test), testToolchain, 2)
	if err != nil {
		test.Fatalf("resolving with an uncommitted edit on disk: %v", err)
	}
	if built || again != first {
		test.Fatalf("an uncommitted Swift edit changed the engine (built=%v)", built)
	}

	// A committed edit builds a new engine, and the build reads the commit even while the checkout
	// holds a different uncommitted edit. A cache hit above could not show that; this fresh build can.
	writeFile(test, fixture.rule(), "two\n")
	fixture.commit(test, "swift")
	writeFile(test, fixture.rule(), "two, uncommitted\n")
	second, built, err := resolveCommittedSwiftEngine(fixture.paths, fixture.head(test), testToolchain, 2)
	if err != nil {
		test.Fatalf("building from the second commit: %v", err)
	}
	if !built || second == first {
		test.Fatalf("a committed Swift edit did not build a new engine (built=%v)", built)
	}
	if output := run(test, second); output != "rule=two\n" {
		test.Fatalf("the engine for the second commit printed %q, so it was not built from that commit", output)
	}

	// A commit that changes only the engine's tests changes nothing compiled in, so it reuses the engine.
	writeFile(test, filepath.Join(fixture.repository, "swift", "Tests", "RuleTests.swift"), "// a new test\n")
	fixture.commit(test, "swift/Tests")
	third, built, err := resolveCommittedSwiftEngine(fixture.paths, fixture.head(test), testToolchain, 2)
	if err != nil {
		test.Fatalf("resolving a tests-only commit: %v", err)
	}
	if built || third != second {
		test.Fatalf("a tests-only commit rebuilt the engine (built=%v)", built)
	}

	if status := gitCommand(test, fixture.paths.SwiftSourceWorktree(), "status", "--porcelain"); status != "" {
		test.Fatalf("the worktree was left dirty:\n%s", status)
	}
}

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and two
// fixtures at once would build with each other's.
func TestAnOlderCommitGetsItsOwnEngineWhileTheCheckoutIsCleanAtANewerOne(test *testing.T) {
	// The case no provenance check can catch. A member commits, the checkout is clean at the new
	// commit, and a gate still running the cohere built from the previous one asks for its engine. A
	// working-tree build would hand it the newer engine, and that engine would honestly report a clean
	// tree. Only building the commit asked for gets this right.
	fixture := newSwiftFixture(test)
	older := fixture.head(test)
	writeFile(test, fixture.rule(), "two\n")
	fixture.commit(test, "swift")

	engine, _, resolveCommittedSwiftEngineError := resolveCommittedSwiftEngine(fixture.paths, older, testToolchain, 2)
	if resolveCommittedSwiftEngineError != nil {
		test.Fatalf("building the older commit's engine: %v", resolveCommittedSwiftEngineError)
	}
	if output := run(test, engine); output != "rule=one\n" {
		test.Fatalf("the older commit got an engine printing %q, which is the newer commit's", output)
	}
}

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and two
// fixtures at once would build with each other's.
func TestADirtySwiftWorktreeIsRefusedNotForced(test *testing.T) {
	fixture := newSwiftFixture(test)
	if _, _, resolveCommittedSwiftEngineError := resolveCommittedSwiftEngine(
		fixture.paths,
		fixture.head(test),
		testToolchain,
		2,
	); resolveCommittedSwiftEngineError != nil {
		test.Fatalf("first build: %v", resolveCommittedSwiftEngineError)
	}

	stray := filepath.Join(fixture.paths.SwiftSourceWorktree(), "swift", "Sources", "Stray.swift")
	writeFile(test, stray, "// someone wrote here\n")
	writeFile(test, fixture.rule(), "two\n")
	fixture.commit(test, "swift")

	_, _, resolveCommittedSwiftEngineError := resolveCommittedSwiftEngine(
		fixture.paths,
		fixture.head(test),
		testToolchain,
		2,
	)
	if resolveCommittedSwiftEngineError == nil ||
		!strings.Contains(resolveCommittedSwiftEngineError.Error(), "changes nobody should have made") {
		test.Fatalf("a dirty worktree was moved or built from: %v", resolveCommittedSwiftEngineError)
	}
	if _, statErr := os.Stat(stray); statErr != nil {
		test.Fatalf("the refusal discarded the stray file, which forcing would do: %v", statErr)
	}
}

// Not parallel: newSwiftFixture swaps the package variable buildSwiftProduct for its own stand-in, and two
// fixtures at once would build with each other's.
func TestACommittedSwiftEngineReportingAModifiedTreeIsNotCached(test *testing.T) {
	fixture := newSwiftFixture(test)
	fixture.reportModified = true

	_, _, resolveCommittedSwiftEngineError := resolveCommittedSwiftEngine(
		fixture.paths,
		fixture.head(test),
		testToolchain,
		2,
	)
	if resolveCommittedSwiftEngineError == nil ||
		!strings.Contains(resolveCommittedSwiftEngineError.Error(), "modified source tree") {
		test.Fatalf("an engine reporting a modified tree was accepted: %v", resolveCommittedSwiftEngineError)
	}
	entries, _ := os.ReadDir(fixture.paths.BinaryDirectory())
	for _, entry := range entries {
		test.Errorf("a refused engine left %s in the cache", entry.Name())
	}
}

// swiftFixture is a repository holding an engine package, with a stand-in for SwiftPM.
type swiftFixture struct {
	repository     string
	paths          Paths
	reportModified bool
}

func newSwiftFixture(test *testing.T) *swiftFixture {
	test.Helper()
	root := test.TempDir()
	repository := filepath.Join(root, "cohere")
	writeFile(test, filepath.Join(repository, "swift", "Package.swift"), "// swift-tools-version:6.0\n")
	writeFile(test, filepath.Join(repository, "swift", "Package.resolved"), "{}\n")
	writeFile(test, filepath.Join(repository, "swift", "Sources", "Rule.swift"), "one\n")
	writeFile(test, filepath.Join(repository, "swift", "Tests", "RuleTests.swift"), "// tests\n")
	writeFile(test, filepath.Join(repository, ".gitignore"), "/.cache/\n")
	gitCommand(test, repository, "init", "-q")
	gitCommand(test, repository, "add", "swift", ".gitignore")
	gitCommand(test, repository, "commit", "-q", "-m", "first")

	fixture := &swiftFixture{
		repository: repository,
		paths:      Paths{ModuleDirectory: repository, CacheDirectory: filepath.Join(repository, ".cache", "cohere")},
	}

	original := buildSwiftProduct
	buildSwiftProduct = fixture.fakeBuild
	test.Cleanup(func() { buildSwiftProduct = original })
	return fixture
}

// fakeBuild writes an engine that prints the rule it was built from and the provenance the real
// manifest would compute for packageDirectory.
func (fixture *swiftFixture) fakeBuild(packageDirectory string, scratchDirectory string, binaryPath string) error {
	rule, err := os.ReadFile(filepath.Join(packageDirectory, "Sources", "Rule.swift"))
	if err != nil {
		return err
	}
	status, err := gitOutput(packageDirectory, "status", "--porcelain")
	if err != nil {
		return err
	}
	modified := strings.TrimSpace(status) != "" || fixture.reportModified

	script := fmt.Sprintf(
		"#!/bin/sh\nif [ \"$3\" = \"--version\" ]; then\n  echo '{\"kind\":\"provenance\",\"sourceTreeModified\":%t}'\n  exit 0\nfi\necho 'rule=%s'\n",
		modified,
		strings.TrimSpace(string(rule)),
	)
	if createDirectoryError := os.MkdirAll(filepath.Dir(binaryPath), 0o755); createDirectoryError != nil {
		return createDirectoryError
	}
	return os.WriteFile(binaryPath, []byte(script), 0o755)
}

func (fixture *swiftFixture) rule() string {
	return filepath.Join(fixture.repository, "swift", "Sources", "Rule.swift")
}

func (fixture *swiftFixture) head(test *testing.T) string {
	test.Helper()
	return strings.TrimSpace(gitCommand(test, fixture.repository, "rev-parse", "HEAD"))
}

func (fixture *swiftFixture) commit(test *testing.T, paths ...string) {
	test.Helper()
	gitCommand(test, fixture.repository, append([]string{"add"}, paths...)...)
	gitCommand(test, fixture.repository, "commit", "-q", "-m", "next")
}
