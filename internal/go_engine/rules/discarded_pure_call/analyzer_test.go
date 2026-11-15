package discarded_pure_call

import (
	"go/types"
	"slices"
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
// holding no file set, so a question of it panics: it must panic at a function taking a *testing.B and nowhere else,
// and must panic at some, which shows the probe would catch it.
func TestCheckAsksTheFileOnlyOfAFunctionTakingABenchmark(test *testing.T) {
	test.Parallel()
	var mutex sync.Mutex
	asked := []string{}
	unasked := 0
	prober := &analysis.Analyzer{
		Name:     "DiscardedPureCallProbe",
		Doc:      "judges every function of a package with Check, in a context whose file set panics when asked",
		Requires: []*analysis.Analyzer{build_intermediate_representation.Analyzer, generated.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			built, readError := build_intermediate_representation.ResultOf(pass)
			if readError != nil {
				return nil, readError
			}
			context, contextError := staticcheck_report.PassContext(pass)
			if contextError != nil {
				return nil, contextError
			}
			context.FileSet = nil
			for function := range built.SourceFunctions() {
				isAsked := func() (isAsked bool) {
					defer func() {
						isAsked = recover() != nil
					}()
					// Nothing is pure, so nothing is reported, and only the question of the file can panic.
					Check(context, function, func(*types.Func) bool { return false })
					return false
				}()
				mutex.Lock()
				if isAsked {
					asked = append(asked, function.String())
					if !takesBenchmark(function) {
						test.Errorf("Check asked whether %s is in a test file, which takes no *testing.B", function)
					}
				} else {
					unasked++
				}
				mutex.Unlock()
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
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{prober}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing parity: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}
	slices.Sort(asked)
	test.Logf("the file set was asked of %d functions, %v, and not of %d", len(asked), asked, unasked)
	if len(asked) == 0 || unasked == 0 {
		test.Fatal("the probe was asked of every function or of none, so it proves nothing")
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
