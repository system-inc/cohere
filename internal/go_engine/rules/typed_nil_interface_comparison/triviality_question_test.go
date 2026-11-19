package typed_nil_interface_comparison_test

import (
	"go/types"
	"slices"
	"sync"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/function_triviality"
	"github.com/system-inc/cohere/internal/go_engine/rules/return_nil_ness"
	"github.com/system-inc/cohere/internal/go_engine/rules/typed_nil_interface_comparison"
)

// heardQuestions is a hand on a run's answers (fact_resolver.Held) that answers nothing and hears each question the run
// would compute, the function's full name by the kind's name. Its methods are called from every pass at once.
type heardQuestions struct {
	lock  sync.Mutex
	asked map[string][]string
}

func (heard *heardQuestions) Answer(kindName string, object types.Object) ([]byte, bool) {
	function, isFunction := object.(*types.Func)
	if !isFunction {
		return nil, false
	}
	heard.lock.Lock()
	defer heard.lock.Unlock()
	if !slices.Contains(heard.asked[kindName], function.FullName()) {
		heard.asked[kindName] = append(heard.asked[kindName], function.FullName())
	}
	return nil, false
}

func (*heardQuestions) Computed(string, types.Object, []byte) {}

// The rule asks a callee's triviality last, and only of a callee the nilness leaves flagged (#h0k644s): over the order
// fixture, it asks the nilness of both typedNil and mayBeNil, whose comparisons it reaches, and the triviality of
// typedNil alone, since mayBeNil may return a nil interface. Asked in the other order the finding is the same, the
// one comparison with typedNil, and only the questions differ: the triviality of mayBeNil would be asked, judged and
// recorded for nothing. So the questions are what this pins.
func TestTheTrivialityIsAskedOnlyOfACalleeTheNilnessFlags(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"order"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the order fixture: %v", loadError)
	}
	// The checker skips a package that does not type-check, which would ask nothing and read green.
	if len(loaded.Roots) == 0 {
		test.Fatal("loading the order fixture found no package")
	}
	for _, root := range loaded.Roots {
		if len(root.Errors) > 0 {
			test.Fatalf("the fixture %s does not load clean: %v", root.ID, root.Errors)
		}
	}
	options := check.DispatchOptions()
	resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
	if !isResolver {
		test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
	}
	heard := &heardQuestions{asked: map[string][]string{}}
	resolver.HoldAnswers(heard)
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{typed_nil_interface_comparison.Analyzer},
		loaded.Roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing the order fixture: %v", analyzeError)
	}
	var reported []string
	for _, action := range graph.Roots {
		if action.Analyzer != typed_nil_interface_comparison.Analyzer {
			continue
		}
		if action.Err != nil {
			test.Fatalf("running the rule on %s: %v", action.Package, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			reported = append(reported, diagnostic.Message)
		}
	}
	if want := []string{"this comparison is always true"}; !slices.Equal(reported, want) {
		test.Errorf("the rule reported %q over the fixture, want %q, the comparison with typedNil", reported, want)
	}
	for _, kind := range []struct {
		name string
		want []string
	}{
		{return_nil_ness.Kind.Name, []string{"order.mayBeNil", "order.typedNil"}},
		{function_triviality.Kind.Name, []string{"order.typedNil"}},
	} {
		asked := slices.Sorted(slices.Values(heard.asked[kind.name]))
		if !slices.Equal(asked, kind.want) {
			test.Errorf("the run asked %s of %q, want %q", kind.name, asked, kind.want)
		}
	}
}
