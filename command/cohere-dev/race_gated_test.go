package main

import (
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
)

// concurrencyTestName is a test whose name says it runs code at once.
var concurrencyTestName = regexp.MustCompile(`^Test\w*(Concurrent|AtOnce|InParallel|Simultaneous)`)

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
		parsed, parseError := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if parseError != nil {
			return parseError
		}
		for _, declaration := range parsed.Decls {
			if function, isFunction := declaration.(*ast.FuncDecl); isFunction && function.Recv == nil &&
				concurrencyTestName.MatchString(function.Name.Name) {
				found[relative] = function.Name.Name
				return nil
			}
		}
		return nil
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	return found
}

// The guard's two patterns read as intended: a name or a file that says it runs at once is found, and a test that
// merely mentions parallel or race in a rule's name is not.
func TestConcurrencyTestsAreFoundByNameAndFile(test *testing.T) {
	test.Parallel()
	for name, want := range map[string]bool{
		"TestConcurrentReadersNeverSeeAPartialFile":        true,
		"TestDependenceSetsComputedAtOnceOverOneHostAgree": true,
		"TestRunCacheRecordsInParallelInTheSerialOrder":    true,
		"TestBuildConcurrently":                            true,
		"TestNoUnclearedRaceTimeout":                       false,
		"TestRequireParallelFlagsASerialTest":              false,
		"TestTheParallelRuleNamesItsCategory":              false,
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
	checkGated := slices.ContainsFunc(raceGated, func(entry raceEntry) bool {
		return entry.directory == "internal/go_engine/check"
	})
	if !checkGated {
		test.Error("internal/go_engine/check, home of the test #bnabymg was written for, is not on the -race list")
	}
}
