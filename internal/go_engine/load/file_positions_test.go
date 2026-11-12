package load

import (
	"fmt"
	"go/token"
	"go/types"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/packages"
)

// widePackageFiles is how many files the wide fixture's package has, besides its one in-package test file, and
// wideFunctions how many functions each holds: a few hundred lines a file, so parses started together overlap.
const (
	widePackageFiles = 12
	wideFunctions    = 60
)

// wideFixture is a module of one package of many large files with an in-package test file, importing nothing, so a
// load parses and checks it alone, in milliseconds, and a test loads it hundreds of times.
func wideFixture() map[string]string {
	files := map[string]string{"wide/go.mod": "module example.com/wide\n\ngo 1.21\n"}
	for index := range widePackageFiles + 1 {
		var contents strings.Builder
		name := fmt.Sprintf("wide/file%02d.go", index)
		if index == widePackageFiles {
			name = "wide/wide_test.go"
		}
		contents.WriteString("package wide\n")
		for function := range wideFunctions {
			fmt.Fprintf(&contents, "\n// Sum%dx%d adds every value below limit that divides by the step.\n", index,
				function)
			fmt.Fprintf(&contents, "func Sum%dx%d(limit int) int {\n\ttotal := 0\n", index, function)
			fmt.Fprintf(&contents, "\tfor value := range limit {\n\t\tif value%%%d == 0 {\n", function+1)
			contents.WriteString("\t\t\ttotal += value\n\t\t}\n\t}\n\treturn total\n}\n")
		}
		files[name] = contents.String()
	}
	return files
}

// wideGraph lists the wide fixture once and returns a function giving a fresh graph of it for each load, so a load is
// only the parse and the check, the part whose order is in question.
func wideGraph(test *testing.T) (func() *graph, string) {
	test.Helper()
	directory := test.TempDir()
	writeListingFiles(test, directory, wideFixture())
	workspace := directory + "/wide"
	goEnvironment := goEnvironmentOf(test, workspace, []string{"GOWORK=off"})
	listed, listError := list(goEnvironment, []string{"./..."}, nil, nil, nil)
	if listError != nil {
		test.Fatal(listError)
	}
	return func() *graph { return newGraph(listed, workspace, nil) }, workspace
}

// newWideLoader is a loader over a fresh graph of the wide fixture, its packages connected.
func newWideLoader(test *testing.T, newGraphOf func() *graph) (*loader, []*node) {
	test.Helper()
	sizes := types.SizesFor("gc", runtime.GOARCH)
	if sizes == nil {
		test.Fatalf("go/types knows no sizes for %s", runtime.GOARCH)
	}
	typeChecker := newLoader(newGraphOf(), sizes, newWithheldErrors(), nil, nil, nil)
	return typeChecker, typeChecker.connect()
}

// outOfOrder is the first pair of a package's files whose bases do not increase in its CompiledGoFiles order, empty
// when every pair does.
func outOfOrder(syntaxFiles []string, bases []int) string {
	for index := 1; index < len(bases); index++ {
		if bases[index] <= bases[index-1] {
			return fmt.Sprintf("%s at base %d is before %s at base %d", syntaxFiles[index], bases[index],
				syntaxFiles[index-1], bases[index-1])
		}
	}
	return ""
}

// basesOf is the base of each of a package's parsed files in the file set, and each one's name, in the order the
// package holds them.
func basesOf(fileSet *token.FileSet, current *node) ([]string, []int) {
	var names []string
	var bases []int
	for _, syntax := range current.loaded.Syntax {
		file := fileSet.File(syntax.FileStart)
		names = append(names, file.Name())
		bases = append(bases, file.Base())
	}
	return names, bases
}

// TestFileBasesFollowEachPackagesOrder loads the wide fixture many times and asserts every package's files take
// bases in its CompiledGoFiles order on every load, whatever order their parses ran in, so a check comparing
// positions across a package's files gives the same answer every run.
func TestFileBasesFollowEachPackagesOrder(test *testing.T) {
	test.Parallel()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Skip("parses never overlap on one core")
	}

	newGraphOf, _ := wideGraph(test)
	const loads = 200
	// Each package is counted apart: the package alone flips only with its parses' scheduling, and its test variant,
	// sharing its files, also with which of the two claims them first.
	failedLoads := map[string]int{}
	firstFailures := map[string]string{}
	for range loads {
		typeChecker, reachable := newWideLoader(test, newGraphOf)
		typeChecker.load(reachable)
		for _, current := range reachable {
			if len(current.loaded.Syntax) != len(current.loaded.CompiledGoFiles) {
				test.Fatalf("%s parsed %d of its %d files: %v", current.loaded.ID, len(current.loaded.Syntax),
					len(current.loaded.CompiledGoFiles), current.loaded.Errors)
			}
			names, bases := basesOf(typeChecker.fileSet, current)
			if failure := outOfOrder(names, bases); failure != "" {
				if failedLoads[current.loaded.ID] == 0 {
					firstFailures[current.loaded.ID] = failure
				}
				failedLoads[current.loaded.ID]++
			}
		}
	}
	for identifier, failed := range failedLoads {
		test.Errorf("%s: %d of %d loads gave its files bases out of their order, first %s", identifier, failed, loads,
			firstFailures[identifier])
	}
}

// TestATestVariantKeepsItsPackagesOrderWhicheverClaimsFirst claims the wide package's files and its test variant's in
// both orders, the later claim's parse started first so it races the earlier's, and asserts both packages' files take
// bases in their own order every time: the files the earlier claim took first, in its order, and the test file the
// test variant holds alone after them, never reserved before the files it follows.
func TestATestVariantKeepsItsPackagesOrderWhicheverClaimsFirst(test *testing.T) {
	test.Parallel()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Skip("parses never overlap on one core")
	}

	newGraphOf, _ := wideGraph(test)
	for _, testVariantFirst := range []bool{false, true} {
		const loads = 100
		failed := 0
		var firstFailure string
		for range loads {
			typeChecker, _ := newWideLoader(test, newGraphOf)
			alone := typeChecker.graph.nodes["example.com/wide"]
			withTests := typeChecker.graph.nodes["example.com/wide [example.com/wide.test]"]
			if alone == nil || withTests == nil {
				test.Fatalf("want example.com/wide and its test variant in the graph, got %v", typeChecker.graph.nodes)
			}
			first, second := alone, withTests
			if testVariantFirst {
				first, second = withTests, alone
			}
			firstClaim := typeChecker.claim(first.loaded.CompiledGoFiles)
			secondClaim := typeChecker.claim(second.loaded.CompiledGoFiles)
			var parsing sync.WaitGroup
			parsing.Go(func() { second.loaded.Syntax, _ = typeChecker.parseClaimed(secondClaim, second) })
			first.loaded.Syntax, _ = typeChecker.parseClaimed(firstClaim, first)
			parsing.Wait()
			for _, current := range []*node{alone, withTests} {
				names, bases := basesOf(typeChecker.fileSet, current)
				if failure := outOfOrder(names, bases); failure != "" {
					if failed == 0 {
						firstFailure = current.loaded.ID + ": " + failure
					}
					failed++
					break
				}
			}
		}
		if failed > 0 {
			test.Errorf(
				"test variant claiming first %t: %d of %d loads gave a package's files bases out of their order, "+
					"first %s",
				testVariantFirst,
				failed,
				loads,
				firstFailure,
			)
		}
	}
}

// TestATestVariantWhoseTestFilesComeBetweenItsPackagesIsTheSameEveryLoad shapes the wide package's test variant as a
// package with cgo files has it, its test file before the package's last file, as the cgo files and the cgo tool's
// outputs come after the tests' (see withTests), and loads it many times. Its files cannot all follow its order then,
// since the package alone holds the last file after the rest of its own; the order they do take depends on which of
// the two claims the shared files, so it must be the same every load.
func TestATestVariantWhoseTestFilesComeBetweenItsPackagesIsTheSameEveryLoad(test *testing.T) {
	test.Parallel()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Skip("parses never overlap on one core")
	}

	newGraphOf, _ := wideGraph(test)
	const loads = 200
	orders := map[string]int{}
	for range loads {
		typeChecker, reachable := newWideLoader(test, newGraphOf)
		withTests := typeChecker.graph.nodes["example.com/wide [example.com/wide.test]"]
		if withTests == nil {
			test.Fatalf("want the test variant of example.com/wide in the graph, got %v", typeChecker.graph.nodes)
		}
		files := withTests.loaded.CompiledGoFiles
		last := len(files) - 1
		withTests.loaded.CompiledGoFiles = slices.Concat(files[:last-1], []string{files[last], files[last-1]})
		typeChecker.load(reachable)
		names, bases := basesOf(typeChecker.fileSet, withTests)
		byBase := slices.Clone(names)
		slices.SortFunc(byBase, func(left, right string) int {
			return bases[slices.Index(names, left)] - bases[slices.Index(names, right)]
		})
		orders[strings.Join(byBase, " ")]++
	}
	if len(orders) > 1 {
		test.Errorf("the test variant's files took %d different orders over %d loads: %v", len(orders), loads, orders)
	}
}

// TestRestoredPackagesCheckedFromSourceClaimInTheSameOrderEveryLoad restores the wide package and its test variant,
// shaped as TestATestVariantWhoseTestFilesComeBetweenItsPackagesIsTheSameEveryLoad shapes it, each decided late and
// each checked from source after all, the package turned down by its decision and the test variant by export data
// that does not read, so both claim their files late, and loads it many times: the order the test variant's files
// take depends on which of the two claims the shared files, so it must be the same every load, as a package checked
// from source from the start has it.
func TestRestoredPackagesCheckedFromSourceClaimInTheSameOrderEveryLoad(test *testing.T) {
	test.Parallel()
	if runtime.GOMAXPROCS(0) < 2 {
		test.Skip("parses never overlap on one core")
	}

	newGraphOf, _ := wideGraph(test)
	const loads = 200
	orders := map[string]int{}
	unreadable := func() ([]byte, error) { return nil, fmt.Errorf("no export data") }
	for range loads {
		typeChecker, reachable := newWideLoader(test, newGraphOf)
		alone := typeChecker.graph.nodes["example.com/wide"]
		withTests := typeChecker.graph.nodes["example.com/wide [example.com/wide.test]"]
		if alone == nil || withTests == nil {
			test.Fatalf("want example.com/wide and its test variant in the graph, got %v", typeChecker.graph.nodes)
		}
		files := withTests.loaded.CompiledGoFiles
		last := len(files) - 1
		withTests.loaded.CompiledGoFiles = slices.Concat(files[:last-1], []string{files[last], files[last-1]})
		turnedDown := func(map[string]*types.Package) bool { return false }
		held := func(map[string]*types.Package) bool { return true }
		typeChecker.restored = map[*packages.Package]*Restored{
			alone.loaded:     {ReadExportData: unreadable, Holds: turnedDown},
			withTests.loaded: {ReadExportData: unreadable, Holds: held},
		}
		typeChecker.load(reachable)
		if len(withTests.loaded.Syntax) != len(withTests.loaded.CompiledGoFiles) {
			test.Fatalf("the test variant parsed %d of its %d files: %v", len(withTests.loaded.Syntax),
				len(withTests.loaded.CompiledGoFiles), withTests.loaded.Errors)
		}
		names, bases := basesOf(typeChecker.fileSet, withTests)
		byBase := slices.Clone(names)
		slices.SortFunc(byBase, func(left, right string) int {
			return bases[slices.Index(names, left)] - bases[slices.Index(names, right)]
		})
		orders[strings.Join(byBase, " ")]++
	}
	if len(orders) > 1 {
		test.Errorf("the test variant's files took %d different orders over %d loads: %v", len(orders), loads, orders)
	}
}
