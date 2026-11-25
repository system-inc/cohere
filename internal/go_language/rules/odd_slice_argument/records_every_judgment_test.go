package odd_slice_argument_test

import (
	"cmp"
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// ownFunctions is every function and method own declares, sorted by full name.
func ownFunctions(own *types.Package) []*types.Func {
	var functions []*types.Func
	scope := own.Scope()
	for _, name := range scope.Names() {
		switch object := scope.Lookup(name).(type) {
		case *types.Func:
			functions = append(functions, object)
		case *types.TypeName:
			if named, isNamed := object.Type().(*types.Named); isNamed {
				functions = append(functions, slices.Collect(named.Methods())...)
			}
		}
	}
	slices.SortFunc(functions, func(left *types.Func, right *types.Func) int {
		return cmp.Compare(left.FullName(), right.FullName())
	})
	return functions
}

// The first half of what the kind's flag claims (fact_resolver.Kind.RecordsEveryJudgment): the mark of every parameter
// of every function SA5012's own state holds settled is an answer the resolver holds, the same one. Over the package's
// fixtures, a run asks every parameter of every other function of each package, by name, and a second run the rest, so
// each judgment settles functions nobody asked of: after each, every settled mark is among the resolver's answers
// (fact_resolver.Answered), and some of them were never asked of, which only a mark handed over (fact_resolver.Keep)
// explains.
func TestEveryMarkOddSliceSettlesIsAnAnswerTheResolverHolds(test *testing.T) {
	test.Parallel()
	if !odd_slice_argument.Kind.RecordsEveryJudgment {
		test.Fatal("the kind does not say it records every judgment, so there is nothing to hold it to")
	}
	patterns := []string{"CheckEvenSliceLength", "parity", "differences", "statements", "order/forward", "order/back",
		"standin/first", "standin/last", "instance/first", "instance/last", "around", "kept", "demand/cycle",
		"recorded/subject"}
	held, unasked := 0, 0
	for parity := range 2 {
		roots := loadFixtures(test, analysistest.TestData(), patterns)
		probe := &analysis.Analyzer{
			Name:       "every_other_function",
			Doc:        "asks the mark of every parameter of every other function of the package",
			Requires:   []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer},
			ResultType: reflect.TypeFor[[]*types.Var](),
			Run: func(pass *analysis.Pass) (any, error) {
				marks, isMarks := pass.ResultOf[odd_slice_argument.MarksAnalyzer].(*odd_slice_argument.Result)
				if !isMarks {
					return nil, fmt.Errorf("the marks' result is a %T", pass.ResultOf[odd_slice_argument.MarksAnalyzer])
				}
				var asked []*types.Var
				for index, function := range ownFunctions(pass.Pkg) {
					if index%2 != parity {
						continue
					}
					for parameter := range function.Signature().Params().Variables() {
						marks.Marked(parameter)
						asked = append(asked, parameter)
					}
				}
				return asked, nil
			},
		}
		options := check.DispatchOptions()
		resolver, isResolver := options.Resolver.(*fact_resolver.Resolver)
		if !isResolver {
			test.Fatalf("the engine's options hold a %T, not a fact resolver", options.Resolver)
		}
		graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, roots, options)
		if analyzeError != nil {
			test.Fatalf("analyzing %v: %v", patterns, analyzeError)
		}
		answered := map[types.Object]string{}
		for _, answer := range resolver.Answered() {
			if answer.Kind == odd_slice_argument.Kind {
				answered[answer.Object] = string(answer.Encoded)
			}
		}
		for _, action := range graph.Roots {
			asked, isAsked := action.Result.([]*types.Var)
			if action.Err != nil || !isAsked {
				test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
			}
			home := resolver.Package(action.Package.Types)
			if home == nil {
				test.Fatalf("the resolver never learned %s", action.Package.ID)
			}
			for parameter, marked := range odd_slice_argument.SettledMarks(home) {
				held++
				if !slices.Contains(asked, parameter) {
					unasked++
				}
				if answer, isAnswered := answered[parameter]; !isAnswered || answer != fmt.Sprint(marked) {
					test.Errorf("%s at %s: the kind holds the mark %t, and the resolver the answer %q (held: %t)",
						parameter.Name(), action.Package.Fset.Position(parameter.Pos()), marked, answer, isAnswered)
				}
			}
		}
	}
	if held == 0 || unasked == 0 {
		test.Errorf("the kind held %d marks, %d of them of parameters nobody asked of, want some of each, or the pin "+
			"holds nothing", held, unasked)
	}
}
