package frame_pointer

import (
	"go/build"
	"go/token"
	"os"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/framepointer"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// judgedTargets are the targets the pass judges, every pairing of the operating systems and the architectures it
// reads, each a `GOOS/GOARCH`. x/tools runs its test only on the target of the machine it runs on, so it proves the
// amd64 rules on an amd64 machine and the arm64 rules on an arm64 one; here every target runs on whatever machine
// runs the tests (see on and onTarget).
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
// original and the port find the same findings, each the same in every field, on each target the pass judges, the
// fixtures loaded for it and go/build's default context naming it (see parity.Target).
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	for _, target := range judgedTargets {
		test.Run(target, func(subtest *testing.T) {
			subtest.Parallel()
			on(target).RequireIdentical(subtest, framepointer.Analyzer, Analyzer, analysistest.TestData(), "a",
				"parity")
		})
	}
}

// On a target the pass does not judge, neither the original nor the port finds anything in the same fixture packages,
// although the load hands each assembly files, which hold findings on the targets it judges, and which the run counts
// so an empty load never passes.
func TestNeitherJudgesAnotherTarget(test *testing.T) {
	test.Parallel()
	for _, target := range unjudgedTargets {
		test.Run(target, func(subtest *testing.T) {
			subtest.Parallel()
			on(target).RequireNeitherFinds(subtest, framepointer.Analyzer, Analyzer, analysistest.TestData(),
				assemblyFiles, "a", "parity")
		})
	}
}

// Neither pass judges the runtime's assembly, which writes the frame pointer in frameless functions by design: the
// runtime loaded on one target of each architecture, both find nothing, though the port's judgment of the same files,
// asked directly, finds writes there, which the run counts, so leaving the runtime alone is proven to leave something.
func TestNeitherJudgesTheRuntime(test *testing.T) {
	test.Parallel()
	for _, target := range []string{"darwin/arm64", "linux/amd64"} {
		test.Run(target, func(subtest *testing.T) {
			subtest.Parallel()
			_, architectureName, _ := strings.Cut(target, "/")
			on(target).RequireNeitherFinds(subtest, framepointer.Analyzer, Analyzer, analysistest.TestData(),
				writesJudgedOn(architectures[architectureName]), "runtime")
		})
	}
}

// on is target, `GOOS/GOARCH`, as the harness loads for it and sets it in go/build's default context, where the
// original and the port read it.
func on(target string) parity.Target {
	operatingSystem, architectureName, _ := strings.Cut(target, "/")
	return parity.On("GOOS="+operatingSystem, "GOARCH="+architectureName)
}

// assemblyFiles counts the assembly files the load handed the packages loaded.
func assemblyFiles(_ testing.TB, loaded []*packages.Package) int {
	counted := 0
	for _, root := range loaded {
		for _, filename := range root.OtherFiles {
			if strings.HasSuffix(filename, ".s") {
				counted++
			}
		}
	}
	return counted
}

// writesJudgedOn counts the writes of the frame pointer the port's judgment, for judged, finds when asked directly in
// the assembly files of the packages loaded, which the pass itself leaves alone.
func writesJudgedOn(judged architecture) func(test testing.TB, loaded []*packages.Package) int {
	return func(test testing.TB, loaded []*packages.Package) int {
		test.Helper()
		writes := 0
		judge := &analysis.Pass{
			Fset: token.NewFileSet(),
			Report: func(analysis.Diagnostic) {
				writes++
			},
		}
		for _, root := range loaded {
			for _, filename := range root.OtherFiles {
				if !strings.HasSuffix(filename, ".s") {
					continue
				}
				content, readError := os.ReadFile(filename)
				if readError != nil {
					test.Fatal(readError)
				}
				file := judge.Fset.AddFile(filename, -1, len(content))
				file.SetLinesForContent(content)
				judgeFile(judge, judged, string(content), file)
			}
		}
		test.Logf("the assembly judged directly holds %d writes", writes)
		return writes
	}
}

// onTarget makes subtest run as on a machine building for target, `GOOS/GOARCH`, for RequireUpstreamWants, which
// takes no target, since analysistest loads under the process's environment: go/build's default context, which the
// original and the port read the target from, and the environment, which the load runs go list under, so a package
// holds the files it holds there. Both are put back when subtest ends. Setenv also refuses a parallel test, and none
// may run beside this one, since the default context is the process's own; every other tier here is parallel, so
// none runs until the upstream tier is done.
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
