package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode"
)

// concurrencyTestName is a test whose name says it runs code at once: side by side, as extra builds and graphs run, and
// beside the load, as the lint and a run's passes do, are the engine's words for it (#2xddz1z).
var concurrencyTestName = regexp.MustCompile(
	`^Test\w*(Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad)`)

// concurrencyTestFile is a test file whose name says its tests run code at once.
var concurrencyTestFile = regexp.MustCompile(`(_concurrency_test\.go|^parallel_\w*_test\.go|_parallel_test\.go)$`)

// Every package holding a test whose name or file says it runs code at once is on the -race list, so a new
// concurrency test is caught by the race detector at the gate from its first landing (#bnabymg), and every entry on
// the list is a package of the module that says why it is there. Read from cohere's own tree, from its source alone.
func TestEveryConcurrencyTestIsRaceGated(test *testing.T) {
	test.Parallel()
	root := filepath.Join("..", "..")
	gated := map[string]bool{}
	for _, entry := range raceGated {
		if strings.TrimSpace(entry.reason) == "" {
			test.Errorf("%s is on the -race list with no reason", entry.directory)
		}
		if information, statError := os.Stat(filepath.Join(root, entry.directory)); statError != nil ||
			!information.IsDir() {
			test.Errorf("%s is on the -race list and is no directory of the module (%v)", entry.directory, statError)
		}
		gated[entry.directory] = true
	}
	found := concurrencyTestDirectories(test, root)
	if len(found) == 0 {
		test.Fatal("no concurrency test was found in the module, so the list was checked against nothing")
	}
	for directory, why := range found {
		if !gated[directory] {
			test.Errorf("%s holds %s, and is not on the -race list in race_gated.go: add it, with why", directory, why)
		}
	}
}

// concurrencyTestDirectories is every directory under root, by its path from root, holding a test whose name or file
// says it runs code at once, with the first such test or file. It leaves out testdata, nested modules, the TypeScript
// checkout and hidden directories, none of which the gate tests as cohere's module.
func concurrencyTestDirectories(test *testing.T, root string) map[string]string {
	test.Helper()
	found := map[string]string{}
	walkError := filepath.WalkDir(root, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil {
			return pathError
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (name == "testdata" || name == "TypeScript" || strings.HasPrefix(name, ".") ||
				strings.HasPrefix(name, "_") || fileExists(filepath.Join(path, "go.mod"))) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, relativeError := filepath.Rel(root, filepath.Dir(path))
		if relativeError != nil {
			return relativeError
		}
		relative = filepath.ToSlash(relative)
		if _, seen := found[relative]; seen {
			return nil
		}
		if concurrencyTestFile.MatchString(name) {
			found[relative] = name
			return nil
		}
		tests, testsError := concurrencyTestsInFile(path)
		if testsError != nil {
			return testsError
		}
		if len(tests) > 0 {
			found[relative] = tests[0]
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return found
}

// concurrencyTestsInFile is every top-level test in the test file at path that the guard calls concurrent: each whose
// name says it runs code at once, and every one when the file's name says its tests do.
func concurrencyTestsInFile(path string) ([]string, error) {
	parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
	if parseError != nil {
		return nil, parseError
	}
	wholeFile := concurrencyTestFile.MatchString(filepath.Base(path))
	tests := []string{}
	for _, declaration := range parsed.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") {
			continue
		}
		if wholeFile || concurrencyTestName.MatchString(function.Name.Name) {
			tests = append(tests, function.Name.Name)
		}
	}
	return tests, nil
}

// concurrencyTestsInDirectory is every test the guard calls concurrent in the test files of directory itself, its
// subdirectories left out, since go test runs a package by its directory alone.
func concurrencyTestsInDirectory(test *testing.T, directory string) []string {
	test.Helper()
	entries, readError := os.ReadDir(directory)
	if readError != nil {
		test.Fatal(readError)
	}
	tests := []string{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		found, foundError := concurrencyTestsInFile(filepath.Join(directory, entry.Name()))
		if foundError != nil {
			test.Fatal(foundError)
		}
		tests = append(tests, found...)
	}
	return tests
}

// raceGatedEntry is the -race list's entry for directory, failing test when the list holds none.
func raceGatedEntry(test *testing.T, directory string) raceEntry {
	test.Helper()
	index := slices.IndexFunc(raceGated, func(entry raceEntry) bool { return entry.directory == directory })
	if index < 0 {
		test.Fatalf("%s is not on the -race list", directory)
	}
	return raceGated[index]
}

// raceRunRefusal is why the -race list can't carry run, a go test -run pattern, for its package's concurrency tests,
// empty when it can: a run the wire or the shell can't carry, one that isn't a regular expression, or one that leaves
// out any of tests, matched as go test matches a top-level test's name.
func raceRunRefusal(run string, tests []string) string {
	// The scripts take the races comma-separated, split a token's run off at its at sign, and hand it single-quoted
	// to the box's shell and double-quoted to go test, and a run with a space would split there.
	if strings.ContainsAny(run, ",@'\"`") || strings.ContainsFunc(run, unicode.IsSpace) {
		return fmt.Sprintf("its run %q holds a comma, an at sign, a quote or a space, which the gate's scripts can't "+
			"carry", run)
	}
	selects, compileError := regexp.Compile(run)
	if compileError != nil {
		return fmt.Sprintf("its run %q is no regular expression: %v", run, compileError)
	}
	missed := []string{}
	for _, name := range tests {
		if !selects.MatchString(name) {
			missed = append(missed, name)
		}
	}
	if len(missed) > 0 {
		return fmt.Sprintf("its run %q leaves out %s, which the guard calls concurrent", run,
			strings.Join(missed, ", "))
	}
	return ""
}

// An entry of the -race list that runs only some of its package's tests runs every one the guard calls concurrent,
// by its name or its file, so a test that says it runs code at once is caught by the race detector at the gate however
// its package's entry narrows the run (#e8w514b), and its run is one the gate's scripts carry whole.
func TestEveryRaceRunSelectsItsPackagesConcurrencyTests(test *testing.T) {
	test.Parallel()
	root := filepath.Join("..", "..")
	for _, entry := range raceGated {
		if entry.run == "" {
			continue
		}
		tests := concurrencyTestsInDirectory(test, filepath.Join(root, entry.directory))
		if refusal := raceRunRefusal(entry.run, tests); refusal != "" {
			test.Errorf("%s is on the -race list, and %s", entry.directory, refusal)
		}
	}
}

// raceRunRefusal refuses each run the wire, the shell or go test can't take, and one that leaves out a concurrency
// test, and takes one that selects every one.
func TestARaceRunIsRefusedWhenItCantBeCarriedOrLeavesATestOut(test *testing.T) {
	test.Parallel()
	tests := []string{"TestExtraBuildsSideBySideWriteWhatOneAtATimeWrites", "TestTheLintBesideTheLoadLeavesNoTrace"}
	for run, refusal := range map[string]string{
		"SideBySide|BesideTheLoad":  "",
		"SideBySide":                "leaves out TestTheLintBesideTheLoadLeavesNoTrace",
		"SideBySide,BesideTheLoad":  "the gate's scripts can't carry",
		"SideBySide@BesideTheLoad":  "the gate's scripts can't carry",
		"SideBySide|'Beside'":       "the gate's scripts can't carry",
		"SideBySide|\"Beside\"":     "the gate's scripts can't carry",
		"SideBySide| BesideTheLoad": "the gate's scripts can't carry",
		"SideBySide|(BesideTheLoad": "is no regular expression",
	} {
		got := raceRunRefusal(run, tests)
		if (refusal == "") != (got == "") || !strings.Contains(got, refusal) {
			test.Errorf("the run %q is refused as %q, want %q", run, got, refusal)
		}
	}
}

// The guard's two patterns read as intended: a name or a file that says it runs at once is found, and a test that
// merely mentions parallel or race in a rule's name is not.
func TestConcurrencyTestsAreFoundByNameAndFile(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{
		"TestConcurrentReadersNeverSeeAPartialFile":                             true,
		"TestDependenceSetsComputedAtOnceOverOneHostAgree":                      true,
		"TestRunCacheRecordsInParallelInTheSerialOrder":                         true,
		"TestBuildConcurrently":                                                 true,
		"TestExtraBuildsSideBySideWriteWhatOneAtATimeWrites":                    true,
		"TestTheLintBesideTheLoadLeavesNoTraceWhenALatePackageDoesNotTypeCheck": true,
		"TestNoUnclearedRaceTimeout":                                            false,
		"TestRequireParallelFlagsASerialTest":                                   false,
		"TestTheParallelRuleNamesItsCategory":                                   false,
		"TestTheBuildsWidthIsReadFromTheMachine":                                false,
	} {
		if got := concurrencyTestName.MatchString(name); got != want {
			test.Errorf("%s reads as a concurrency test: %t, want %t", name, got, want)
		}
	}
	for name, want := range map[string]bool{
		"dependence_concurrency_test.go":                true,
		"parallel_build_test.go":                        true,
		"fix_parallel_test.go":                          true,
		"concurrency_no_lost_update_test.go":            false,
		"correctness_no_uncleared_race_timeout_test.go": false,
		"analyzer_test.go":                              false,
	} {
		if got := concurrencyTestFile.MatchString(name); got != want {
			test.Errorf("%s reads as a concurrency test file: %t, want %t", name, got, want)
		}
	}
	for directory, home := range map[string]string{
		"internal/go_language/check": "TestDependenceSetsComputedAtOnceOverOneHostAgree",
		// Its test of the early start's handoff can fail only under -race (#e8w514b), and the package's entry runs
		// only the tests the guard calls concurrent, so the test's name says it runs code at once.
		"internal/go_language": "TestTheHostsGoEnvironmentIsWholeWhenALoadReadsItAtOnceWithTheEarlyStart",
	} {
		index := slices.IndexFunc(raceGated, func(entry raceEntry) bool { return entry.directory == directory })
		if index < 0 {
			test.Errorf("%s, home of %s, is not on the -race list", directory, home)
			continue
		}
		// Read from the package itself, so a home renamed or moved away is caught here rather than left named.
		if !slices.Contains(concurrencyTestsInDirectory(test, filepath.Join("..", "..", directory)), home) {
			test.Errorf("%s is no test of %s the guard calls concurrent", home, directory)
		}
		if run := raceGated[index].run; run != "" && !regexp.MustCompile(run).MatchString(home) {
			test.Errorf("%s's run on the -race list, %q, leaves out %s", directory, run, home)
		}
	}
}
