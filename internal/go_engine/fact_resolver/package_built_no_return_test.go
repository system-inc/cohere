package fact_resolver_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/system-inc/go-types/types"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/ctrlflow"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
)

// A learned dependency's functions are built with its own no-return predicate, ctrlflow's (#kcy46qb), so a kind judges
// the blocks a build of the package alone holds: a pass on stopping asks the nil-ness of under.Checked, which under,
// learned as ctrlflow finished there and never a root, judges in a package the resolver creates for it. Checked's nil
// return follows a call of Abort, which never returns, so it is cut off, and Checked's error is never nil. Built
// without the predicate, the nil return is reached, its error may be nil, and the answer is no fact.
func TestALearnedDependencysFunctionsAreBuiltWithItsNoReturn(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"stopping"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil || len(loaded.Roots) != 1 {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	options := check.DispatchOptions()
	options.Resolver = fact_resolver.New(nil)
	asking := &analysis.Analyzer{
		Name:       "checked_probe",
		Doc:        "asks the nil-ness of under.Checked, with its package as the reader",
		Requires:   []*analysis.Analyzer{ctrlflow.Analyzer},
		ResultType: reflect.TypeFor[[]byte](),
		Run: func(pass *analysis.Pass) (any, error) {
			resolver, resolverError := fact_resolver.Of(pass)
			if resolverError != nil {
				return nil, resolverError
			}
			reader := resolver.Package(pass.Pkg)
			if reader == nil {
				return nil, fmt.Errorf("the resolver never learned %s", pass.Pkg.Path())
			}
			checked, isFunction := pass.Pkg.Imports()[0].Scope().Lookup("Checked").(*types.Func)
			if !isFunction {
				return nil, fmt.Errorf("%s declares no function Checked", pass.Pkg.Imports()[0].Path())
			}
			encoded, sure, registered := resolver.Asked(return_nil_ness.Kind.Name, reader, checked)
			if !sure || !registered {
				return nil, fmt.Errorf("Checked's nil-ness was asked sure %t of a kind registered %t", sure,
					registered)
			}
			return encoded, nil
		},
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{asking}, loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	action := graph.Roots[0]
	if action.Err != nil {
		test.Fatalf("asking in %s: %v", action.Package.ID, action.Err)
	}
	encoded, isEncoded := action.Result.([]byte)
	if want := `[{"Inner":1,"Outer":1}]`; !isEncoded || string(encoded) != want {
		test.Errorf("under.Checked's nil-ness is %q, want %s, its error never nil: the nil return after Abort, which "+
			"never returns, is never reached", encoded, want)
	}
}
