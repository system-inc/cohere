package frame_pointer

import (
	"go/build"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/framepointer"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
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

// x/tools' own fixtures for framepointer, copied unchanged into testdata/src, hold the port to every want the original
// expects, and to nothing more, on each target it judges. x/tools' test skips every operating system but linux and
// darwin, since asm_amd64.s holds wants that hold only there, and no target here is another.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesUpstreamWants(test *testing.T) {
	for _, target := range judgedTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			parity.RequireUpstreamWants(subtest, Analyzer, analysistest.TestData(), "a")
		})
	}
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment on each architecture, the
// original and the port find the same findings, each the same in every field, on each target the pass judges. The
// harness loads the fixtures under the environment onTarget set, so each target's load holds that target's files.
//
// Not parallel: each target is set in go/build's default context and the environment, which are the process's own.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	for _, target := range judgedTargets {
		// Not parallel: onTarget sets the target in go/build's default context and the environment, the process's own.
		test.Run(target, func(subtest *testing.T) {
			onTarget(subtest, target)
			parity.RequireIdentical(subtest, framepointer.Analyzer, Analyzer, analysistest.TestData(), "a", "parity")
		})
	}
}

// On a target the pass does not judge, neither the original nor the port finds anything in the same fixture packages,
// although the load hands each assembly files, which hold findings on the targets it judges. It keeps its own load and
// comparison, on parity's pieces, since RequireIdentical fails on an original that finds nothing, which is what this
// tier expects, and the harness does not hand back the load whose assembly files it counts.
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
// asked directly, finds at least one write. It keeps its own load and comparison, on parity's pieces, for the reasons
// TestNeitherJudgesAnotherTarget does, the load also handing the runtime's assembly files to the direct judgment.
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

// compareFindings fails the test for every finding one side makes and the other does not, each the same in every
// field.
func compareFindings(test *testing.T, original []parity.Finding, port []parity.Finding) {
	test.Helper()
	for _, missing := range parity.Difference(original, port) {
		test.Errorf("the original found %s, and the port did not", missing)
	}
	for _, extra := range parity.Difference(port, original) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
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
	for _, failure := range parity.LoadFailures(loaded.Roots) {
		test.Fatalf("fixture package %s does not load clean, so its findings would go uncounted: %s",
			failure.Package, failure.Error)
	}
	return loaded
}

// findingsIn runs x/tools' framepointer and the port over loaded's roots in one checker, and returns each one's
// findings, sorted, each in its file's base name. An analyzer that did not run on a root fails the test.
func findingsIn(test *testing.T, loaded *load.Loaded) (original []parity.Finding, port []parity.Finding) {
	test.Helper()
	outcomes, analyzeError := parity.Analyze(
		[]parity.Pair{{Name: "framepointer", Original: framepointer.Analyzer, Port: Analyzer}}, loaded.Roots,
	)
	if analyzeError != nil {
		test.Fatalf("%v", analyzeError)
	}
	outcome := outcomes[0]
	for _, failure := range append(outcome.Original.Failures, outcome.Port.Failures...) {
		test.Fatalf("running on %s: %s", failure.Package, failure.Error)
	}
	for _, found := range outcome.Original.Findings {
		original = append(original, found.WithFileNames(filepath.Base))
	}
	for _, found := range outcome.Port.Findings {
		port = append(port, found.WithFileNames(filepath.Base))
	}
	return original, port
}
