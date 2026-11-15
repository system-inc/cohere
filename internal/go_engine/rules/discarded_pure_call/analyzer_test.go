package discarded_pure_call

import (
	"bytes"
	"go/types"
	"runtime/debug"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/go/types/typeutil"
	"honnef.co/go/tools/staticcheck/sa4017"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// upstream is staticcheck's own fixture for SA4017, copied unchanged from
// staticcheck/sa4017/testdata/go1.0/CheckSideEffectFreeCalls into testdata/src/CheckSideEffectFreeCalls, its test file
// included. The go1.0 folder sets the language version of staticcheck's run, which SA4017 never reads, so the fixture
// loads from a tree laid out by import path as the house's fixtures do.
const upstream = "CheckSideEffectFreeCalls"

// staticcheck's own fixture holds the port to every `//@ diag(...)` note the original expects, and to nothing more.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireStaticcheckWants(test, Analyzer, analysistest.TestData(), upstream)
}

// Over staticcheck's fixture, parity's and one at the import path fmt_test, which the original spares fmt.Sprintf in,
// the original and the port find the same findings. parity holds pure and impure calls, calls with no static callee,
// results read and discarded, and the benchmark exception in a test file, in an external test file, outside one and
// under a line directive naming one.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4017.Analyzer, Analyzer, analysistest.TestData(), upstream, "parity", "fmt_test")
}

// The differences fixture holds the calls where the purity judgment could most plausibly part. It records no
// difference, since the port parts from the original nowhere, so the two must agree there finding for finding.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, sa4017.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// Check asks whether a function is in a test file only once it takes a *testing.B, so the file set's lookup is spared
// for every other function. Over parity, its tests and its external tests, each function is judged with a context
// holding no file set, so IsInTest's question of it panics, and only a panic whose stack holds IsInTest asking the file
// set counts as the question: Check must ask it of every function taking a *testing.B and of no other, and must panic
// in no other way.
func TestCheckAsksTheFileOnlyOfAFunctionTakingABenchmark(test *testing.T) {
	test.Parallel()
	asked := 0
	unasked := 0
	eachParityFunction(test, func(pass *analysis.Pass, function *intermediate_representation.Function) {
		context, contextError := staticcheck_report.PassContext(pass)
		if contextError != nil {
			// Not Fatal: the probe runs on the analyzer's goroutine, which FailNow must never be called from.
			test.Error(contextError)
			return
		}
		context.FileSet = nil
		isAsked := false
		func() {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				if !isFileAsked(debug.Stack()) {
					test.Errorf("Check panicked at %s other than by asking the file: %v", function, recovered)
					return
				}
				isAsked = true
			}()
			// Nothing is pure, so nothing is reported, and only the question of the file can panic.
			Check(context, function, func(*types.Func) bool { return false })
		}()
		switch {
		case isAsked && !takesBenchmark(function):
			test.Errorf("Check asked whether %s is in a test file, which takes no *testing.B", function)
		case !isAsked && takesBenchmark(function):
			test.Errorf("Check never asked whether %s, which takes a *testing.B, is in a test file", function)
		case isAsked:
			asked++
		default:
			unasked++
		}
	})
	test.Logf("the file set was asked of %d functions and not of %d", asked, unasked)
	if asked == 0 || unasked == 0 {
		test.Fatal("the probe was asked of every function or of none, so it proves nothing")
	}
}

// Check takes IsInTest's answer as it is: judged with every function pure, a function taking a *testing.B reports
// nothing where the file it was parsed from is a test file, and reports its discarded calls where it is not, so a check
// that ignored the answer or read it backwards reports the other way.
func TestCheckSparesABenchmarkOnlyInATestFile(test *testing.T) {
	test.Parallel()
	spared := 0
	judged := 0
	eachParityFunction(test, func(pass *analysis.Pass, function *intermediate_representation.Function) {
		if !takesBenchmark(function) {
			return
		}
		reports := 0
		context := rule.NewContext("SA4017", pass, pass.Analyzer.Requires, &pass.Analyzer.Flags,
			func(string) bool { return false },
			func(string, analysis.Diagnostic) { reports++ })
		Check(context, function, func(*types.Func) bool { return true })
		// The name the file was parsed from, read here without IsInTest, as IsInTest reads it.
		isInTestFile := strings.HasSuffix(pass.Fset.File(function.Pos()).Name(), "_test.go")
		switch {
		case isInTestFile && reports > 0:
			test.Errorf("Check reported %d calls in %s, a benchmark in a test file", reports, function)
		case !isInTestFile && reports == 0:
			test.Errorf("Check reported nothing in %s, which takes a *testing.B outside a test file", function)
		case isInTestFile:
			spared++
		default:
			judged++
		}
	})
	test.Logf("Check spared %d benchmarks in test files and judged %d outside them", spared, judged)
	if spared == 0 || judged == 0 {
		test.Fatal("the fixture held benchmarks only in test files or only outside them, so the probe proves nothing")
	}
}

// eachParityFunction calls visit with each source function of parity, its tests and its external tests, and the pass
// it was built in, one at a time, though the packages are analyzed side by side.
func eachParityFunction(
	test *testing.T,
	visit func(pass *analysis.Pass, function *intermediate_representation.Function),
) {
	test.Helper()
	var mutex sync.Mutex
	visitor := &analysis.Analyzer{
		Name:     "DiscardedPureCallProbe",
		Doc:      "hands each source function of a package to a probe of Check",
		Requires: []*analysis.Analyzer{build_intermediate_representation.Analyzer, generated.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			built, readError := build_intermediate_representation.ResultOf(pass)
			if readError != nil {
				return nil, readError
			}
			mutex.Lock()
			defer mutex.Unlock()
			for function := range built.SourceFunctions() {
				visit(pass, function)
			}
			return nil, nil
		},
	}
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"parity"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading parity: %v", loadError)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{visitor}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing parity: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
}

// takesBenchmark is whether one of function's parameters is a *testing.B.
func takesBenchmark(function *intermediate_representation.Function) bool {
	for parameter := range function.Signature.Params().Variables() {
		if typeutil.IsPointerToTypeWithName(parameter.Type(), "testing.B") {
			return true
		}
	}
	return false
}

// isFileAsked is whether stack, read in a deferred function as a panic unwinds through it, so the panicking frames are
// still on it, is IsInTest asking the file set which file a position is in: any other panic is no question of the
// file.
func isFileAsked(stack []byte) bool {
	return bytes.Contains(stack, []byte("staticcheck_code.IsInTest(")) &&
		bytes.Contains(stack, []byte("go/token.(*FileSet).File("))
}
