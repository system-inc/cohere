package frame_pointer

import (
	"cmp"
	"fmt"
	"go/build"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/framepointer"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
)

// judgedTargets are the targets the pass judges, every pairing of the operating systems and the architectures it
// reads, each a `GOOS/GOARCH`. x/tools runs its test only on the target of the machine it runs on, so it proves the
// amd64 rules on an amd64 machine and the arm64 rules on an arm64 one; here every target runs on whatever machine
// runs the tests (see onTarget).
var judgedTargets = []string{"darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64"}

// unjudgedTargets are targets the pass leaves alone: an operating system it does not judge with each architecture it
// does, and an architecture it does not judge on an operating system it does.
var unjudgedTargets = []string{"windows/amd64", "freebsd/arm64", "linux/riscv64"}

// x/tools' own fixtures for framepointer, copied unchanged into testdata/src and run as x/tools' test runs them, with
// no flags, on each target it judges: every `// want` the original expects, the port reports at the same line with
// the same message, and analysistest fails on a finding no want expects as much as on a want nothing found, so the
// port adds nothing there either. x/tools' test skips every operating system but linux and darwin, since asm_amd64.s
// holds wants that hold only there, and no target here is another.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	for _, target := range judgedTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			analysistest.Run(errorsOnly{subtest}, analysistest.TestData(), Analyzer, "a")
		})
	}
}

// The original and the port run in one checker over the same fixture packages, x/tools' and parity's, the second
// holding every branch of the judgment on each architecture, and their findings must be the same multiset of file,
// line, column, end and message, on each target the pass judges. Each side's count is logged, since two empty sides
// would match and prove nothing, and the test fails if the original finds nothing at all.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	for _, target := range judgedTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			original, port := findingsOf(subtest, "a", "parity")
			subtest.Logf("on %s, the original found %d, the port %d", target, len(original), len(port))
			if len(original) == 0 {
				subtest.Fatal("the original found nothing in the fixtures, so the comparison proves nothing")
			}
			compareFindings(subtest, original, port)
		})
	}
}

// On a target the pass does not judge, neither the original nor the port finds anything in the same fixture packages,
// although the load hands each assembly files, which hold findings on the targets it judges.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestNeitherJudgesAnotherTarget(test *testing.T) {
	for _, target := range unjudgedTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			loaded := loadedFixtures(subtest, "a", "parity")
			assembly := 0
			for _, root := range loaded.Roots {
				for _, filename := range root.OtherFiles {
					if strings.HasSuffix(filename, ".s") {
						assembly++
					}
				}
			}
			if assembly == 0 {
				subtest.Fatalf("the load on %s hands neither pass an assembly file, so the comparison proves nothing",
					target)
			}
			original, port := findingsIn(subtest, loaded)
			subtest.Logf("on %s, over %d assembly files, the original found %d, the port %d", target, assembly,
				len(original), len(port))
			if len(original) != 0 {
				subtest.Errorf("the original judged %s, which it never judges: %v", target, original)
			}
			compareFindings(subtest, original, port)
		})
	}
}

// Neither pass judges the runtime's assembly, which writes the frame pointer in frameless functions by design: the
// runtime loaded on one target of each architecture, both find nothing, though the port's judgment of the same files,
// asked directly, finds at least one write.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestNeitherJudgesTheRuntime(test *testing.T) {
	for _, target := range []string{"darwin/arm64", "linux/amd64"} {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			_, architectureName, _ := strings.Cut(target, "/")
			onTarget(subtest, target)
			loaded := loadedFixtures(subtest, "runtime")
			original, port := findingsIn(subtest, loaded)
			subtest.Logf("on %s, over the runtime, the original found %d, the port %d", target, len(original),
				len(port))
			if len(original) != 0 {
				subtest.Errorf("the original judged the runtime: %v", original)
			}
			compareFindings(subtest, original, port)
			writes := 0
			judge := &analysis.Pass{
				Fset: token.NewFileSet(),
				Report: func(analysis.Diagnostic) {
					writes++
				},
			}
			for _, root := range loaded.Roots {
				for _, filename := range root.OtherFiles {
					if !strings.HasSuffix(filename, ".s") {
						continue
					}
					content, readError := os.ReadFile(filename)
					if readError != nil {
						subtest.Fatal(readError)
					}
					file := judge.Fset.AddFile(filename, -1, len(content))
					file.SetLinesForContent(content)
					judgeFile(judge, architectures[architectureName], string(content), file)
				}
			}
			subtest.Logf("on %s, the runtime's assembly judged directly holds %d writes", target, writes)
			if writes == 0 {
				subtest.Fatal("the runtime's assembly holds no write the judgment finds, so leaving it alone proves " +
					"nothing")
			}
		})
	}
}

// onTarget makes subtest run as on a machine building for target, `GOOS/GOARCH`: go/build's default context, which
// the original and the port read the target from, and the environment, which the load runs go list under, so a
// package holds the files it holds there. Both are put back when subtest ends. Setenv also refuses a parallel test,
// and none may run beside this one, since the default context is the process's own.
func onTarget(subtest *testing.T, target string) {
	subtest.Helper()
	operatingSystem, architectureName, _ := strings.Cut(target, "/")
	subtest.Setenv("GOOS", operatingSystem)
	subtest.Setenv("GOARCH", architectureName)
	savedOperatingSystem, savedArchitecture := build.Default.GOOS, build.Default.GOARCH
	build.Default.GOOS, build.Default.GOARCH = operatingSystem, architectureName
	subtest.Cleanup(func() {
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

// findingsOf loads the fixture packages patterns name, runs x/tools' framepointer and the port over them in one
// checker, and returns each one's findings, sorted.
func findingsOf(test *testing.T, patterns ...string) (original []finding, port []finding) {
	test.Helper()
	return findingsIn(test, loadedFixtures(test, patterns...))
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
	// nothing and read green. Every root must load clean, and there must be one.
	if len(loaded.Roots) == 0 {
		test.Fatalf("loading %v found no package, so the comparison would prove nothing", patterns)
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %v", root.ID,
				root.Errors)
		}
	}
	return loaded
}

// findingsIn runs x/tools' framepointer and the port over loaded's roots in one checker, and returns each one's
// findings, sorted.
func findingsIn(test *testing.T, loaded *load.Loaded) (original []finding, port []finding) {
	test.Helper()
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{framepointer.Analyzer, Analyzer}, loaded.Roots, &checker.Options{},
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
			case framepointer.Analyzer:
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
