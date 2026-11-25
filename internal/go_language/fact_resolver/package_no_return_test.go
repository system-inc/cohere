package fact_resolver_test

import (
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/load"
)

// A package's no-return predicate is ctrlflow's as the package's analysis read it (#aswfm7a): its own functions that
// never return, the unexported halt among them, whose fact ctrlflow never exports, and under.Abort, of another
// package, which it calls and so imported the fact of. Goes returns, and under.Unused, which never returns, is no
// callee of the package, so ctrlflow never read its fact there.
func TestAPackagesNoReturnIsCtrlflowsAsItsAnalysisReadIt(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"stopping"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	resolver := fact_resolver.New(nil)
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{ctrlflow.Analyzer}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running ctrlflow on %s: %v", action.Package.ID, action.Err)
		}
	}
	stopping := resolver.Package(loaded.Roots[0].Types)
	if stopping == nil {
		test.Fatal("the resolver never learned stopping, which ctrlflow ran on")
	}
	under := loaded.Roots[0].Imports["stopping/under"]
	if under == nil {
		test.Fatal("stopping imports no stopping/under")
	}
	for _, function := range []struct {
		scope    *types.Scope
		name     string
		noReturn bool
	}{
		{scope: stopping.Types.Scope(), name: "Stop", noReturn: true},
		{scope: stopping.Types.Scope(), name: "halt", noReturn: true},
		{scope: stopping.Types.Scope(), name: "Goes", noReturn: false},
		{scope: under.Types.Scope(), name: "Abort", noReturn: true},
		{scope: under.Types.Scope(), name: "Unused", noReturn: false},
	} {
		declared, isFunction := function.scope.Lookup(function.name).(*types.Func)
		if !isFunction {
			test.Fatalf("the fixture declares no function %s", function.name)
		}
		if noReturn := stopping.NoReturn(declared); noReturn != function.noReturn {
			test.Errorf("stopping's no-return of %s is %t, want %t", declared.FullName(), noReturn, function.noReturn)
		}
	}
}
