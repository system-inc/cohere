package asm_decl

import (
	"cmp"
	"fmt"
	"go/build"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/asmdecl"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// upstreamTargets are the targets x/tools' test runs its fixtures on, each a `GOOS/GOARCH`, and three more. x/tools
// runs linux/mips although no fixture of its loads there. It leaves out linux/loong64, since its test sets only the
// environment, which go/build's default context has read before the test starts, so asm10.s, which names its
// architecture in a `//go:build` line the pass does not read, would be judged as the machine running the test;
// onTarget sets the default context too, so asm10.s is judged as loong64. And no target of x/tools' loads asm8.s,
// mipsle's, so linux/mipsle is added, and linux/ppc64le, the second architecture asm7.s's build line names.
var upstreamTargets = []string{
	"linux/amd64",
	"linux/386",
	"linux/arm",
	"linux/mips64",
	"linux/s390x",
	"linux/ppc64",
	"linux/mips",
	"js/wasm",
	"linux/riscv64",
	"linux/mipsle",
	"linux/ppc64le",
	"linux/loong64",
}

// architectureTargets are a target for each architecture the pass judges, in its table's order.
var architectureTargets = []string{
	"linux/386",
	"linux/arm",
	"linux/arm64",
	"linux/amd64",
	"linux/mips",
	"linux/mipsle",
	"linux/mips64",
	"linux/mips64le",
	"linux/ppc64",
	"linux/ppc64le",
	"linux/riscv64",
	"linux/s390x",
	"js/wasm",
	"linux/loong64",
}

// fixturePackages are the packages the differential runs over: x/tools' and parity's.
var fixturePackages = []string{"a", "parity", "parity/nested"}

// x/tools' own fixtures for asmdecl, copied unchanged into testdata/src and run as x/tools' test runs them, with no
// flags, on each target its test runs (see upstreamTargets): every `// want` the original expects, the port reports
// at the same line with the same message, and analysistest fails on a finding no want expects as much as on a want
// nothing found, so the port adds nothing there either.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	for _, target := range upstreamTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target, "")
			analysistest.Run(errorsOnly{subtest}, analysistest.TestData(), Analyzer, "a")
		})
	}
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment and an assembly file for each architecture, and their findings must be the
// same multiset of file, line, column, end and message, on a target of every architecture the pass judges. Each
// side's count is logged, since two empty sides would match and prove nothing, and the test fails if the original
// finds nothing at all.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	for _, target := range architectureTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target, "")
			original, port := findingsIn(subtest, loadedFixtures(subtest, fixturePackages...))
			subtest.Logf("on %s, the original found %d, the port %d", target, len(original), len(port))
			if len(original) == 0 {
				subtest.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
			}
			compareFindings(subtest, original, port)
		})
	}
}

// When go/build's default context names an architecture the pass does not know, a file whose name and build lines
// name none cannot be judged, and both leave it whole, the original after logging that it cannot tell the file's
// architecture, while both still judge the files that name theirs. The load is for linux/amd64, and the default
// context says sparc64, which no Go release builds for.
//
// Not parallel: the target is set in go/build's default context and the environment, which are the process's own.
func TestNeitherJudgesAFileOfAnUnknownArchitecture(test *testing.T) {
	onTarget(test, "linux/amd64", "sparc64")
	original, port := findingsIn(test, loadedFixtures(test, fixturePackages...))
	test.Logf("with an unknown default architecture, the original found %d, the port %d", len(original), len(port))
	if len(original) == 0 {
		test.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
	}
	compareFindings(test, original, port)
	// These files name no architecture of their own, and each holds findings on every target that judges it.
	unjudged := []string{"parity.s", "parity_terms.s", "parity_go_build.s"}
	for _, found := range port {
		if slices.Contains(unjudged, found.file) {
			test.Errorf("the port judged %s, whose architecture it cannot tell: %s", found.file, described(found))
		}
	}
}

// onTarget makes test run as on a machine building for target, `GOOS/GOARCH`: go/build's default context, which
// the original and the port read the architecture from, and the environment, which the load runs go list under, so
// a package holds the files it holds there. A defaultArchitecture other than empty is put in the default context in
// place of target's. Both are put back when test ends. Setenv also refuses a parallel test, and none may run beside
// this one, since the default context is the process's own.
func onTarget(test *testing.T, target string, defaultArchitecture string) {
	test.Helper()
	operatingSystem, architectureName, _ := strings.Cut(target, "/")
	test.Setenv("GOOS", operatingSystem)
	test.Setenv("GOARCH", architectureName)
	savedOperatingSystem, savedArchitecture := build.Default.GOOS, build.Default.GOARCH
	build.Default.GOOS, build.Default.GOARCH = operatingSystem, cmp.Or(defaultArchitecture, architectureName)
	test.Cleanup(func() {
		build.Default.GOOS, build.Default.GOARCH = savedOperatingSystem, savedArchitecture
	})
}

// compareFindings fails the test for every finding one side makes and the other does not.
func compareFindings(test *testing.T, original []finding, port []finding) {
	test.Helper()
	for _, missing := range difference(original, port) {
		test.Errorf("the original found %s, and the port did not", described(missing))
	}
	for _, extra := range difference(port, original) {
		test.Errorf("the port found %s, and the original did not", described(extra))
	}
}

// finding is one finding as the differential compares it. Neither side gives a finding an end, so both ends are
// zero, and a side that gave one would differ.
type finding struct {
	file      string
	line      int
	column    int
	endLine   int
	endColumn int
	message   string
}

// described is found as a finding line prints it.
func described(found finding) string {
	return fmt.Sprintf("%s:%d:%d-%d:%d: %s", found.file, found.line, found.column, found.endLine, found.endColumn,
		found.message)
}

// loadedFixtures loads the packages patterns name from the fixture tree, under the target onTarget set, and fails the
// test unless every root loads clean.
func loadedFixtures(test *testing.T, patterns ...string) *load.Loaded {
	test.Helper()
	directory := analysistest.TestData()
	// The fixtures are a tree laid out by import path, loaded as analysistest loads one. `GOOS` and `GOARCH` come from
	// the environment onTarget set.
	loaded, loadError := load.Packages(directory, patterns, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading %v: %v", patterns, loadError)
	}
	// The checker passes over a root that does not load without a word, so a fixture that fails to would compare
	// nothing and read green. Every root must load clean, and there must be one for each pattern.
	if len(loaded.Roots) != len(patterns) {
		test.Fatalf("loading %v found %d packages, so the comparison would not cover them all", patterns,
			len(loaded.Roots))
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	return loaded
}

// findingsIn runs x/tools' asmdecl and the port over loaded's roots in one checker, and returns each one's findings,
// sorted.
func findingsIn(test *testing.T, loaded *load.Loaded) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{asmdecl.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			found := finding{
				file:    filepath.Base(position.Filename),
				line:    position.Line,
				column:  position.Column,
				message: diagnostic.Message,
			}
			if diagnostic.End.IsValid() {
				end := action.Package.Fset.Position(diagnostic.End)
				found.endLine, found.endColumn = end.Line, end.Column
			}
			switch action.Analyzer {
			case asmdecl.Analyzer:
				original = append(original, found)
			case Analyzer:
				port = append(port, found)
			}
		}
	}
	compare := func(first finding, second finding) int {
		return cmp.Compare(described(first), described(second))
	}
	slices.SortFunc(original, compare)
	slices.SortFunc(port, compare)
	return original, port
}

// difference is what of from is not in without, counting repeats, so a finding reported twice by one side and once by
// the other is a difference.
func difference(from []finding, without []finding) []finding {
	remaining := map[finding]int{}
	for _, found := range without {
		remaining[found]++
	}
	missing := []finding{}
	for _, found := range from {
		if remaining[found] > 0 {
			remaining[found]--
			continue
		}
		missing = append(missing, found)
	}
	return missing
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
