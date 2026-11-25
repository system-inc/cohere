package odd_slice_argument_test

import (
	"fmt"
	"go/types"
	"maps"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/types/objectpath"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/check/analysistest"
	"github.com/system-inc/cohere/internal/go_language/check/checker"
	"github.com/system-inc/cohere/internal/go_language/fact_resolver"
	"github.com/system-inc/cohere/internal/go_language/rules/odd_slice_argument"
)

// keptMarks is what one question left in the resolver of a fixture package: the resolver, the package as it holds it,
// every mark it recorded for the package, encoded, by its parameter's objectpath, every mark it holds about the
// parameters of the package's functions (fact_resolver.Answered), encoded alike, each by its function's name and its
// own, as `Function.parameter`, and the parameter each is about.
type keptMarks struct {
	resolver   *fact_resolver.Resolver
	home       *fact_resolver.Package
	recorded   map[string]string
	answered   map[string]string
	parameters map[string]*types.Var
}

// askMarkOnce runs a probe on the fixture package named fixture that asks the resolver the mark of the first parameter
// of its function named asked, and of nothing else, and is what the resolver recorded and holds for the package. The
// run has a store, which answers nothing, since only a run with a store records its answers (fact_resolver.Recorded).
func askMarkOnce(test *testing.T, fixture string, asked string) keptMarks {
	test.Helper()
	roots := loadFixtures(test, analysistest.TestData(), []string{fixture})
	probe := &analysis.Analyzer{
		Name:     "kept_marks_probe",
		Doc:      "asks the mark of one parameter of the package and of nothing else",
		Requires: []*analysis.Analyzer{odd_slice_argument.MarksAnalyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			marks, isMarks := pass.ResultOf[odd_slice_argument.MarksAnalyzer].(*odd_slice_argument.Result)
			if !isMarks {
				return nil, fmt.Errorf("the marks' result is a %T", pass.ResultOf[odd_slice_argument.MarksAnalyzer])
			}
			function, isFunction := pass.Pkg.Scope().Lookup(asked).(*types.Func)
			if !isFunction {
				return nil, fmt.Errorf("%s declares no function %s", fixture, asked)
			}
			marks.Marked(function.Signature().Params().At(0))
			return nil, nil
		},
	}
	resolver := fact_resolver.New(emptyStore{})
	options := check.DispatchOptions()
	options.Resolver = resolver
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{probe}, roots, options)
	if analyzeError != nil {
		test.Fatalf("analyzing %s: %v", fixture, analyzeError)
	}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("probing %s: %v", action.Package.ID, action.Err)
		}
	}
	kept := keptMarks{resolver: resolver, home: resolver.Package(roots[0].Types), recorded: map[string]string{},
		answered: map[string]string{}, parameters: map[string]*types.Var{}}
	if kept.home == nil {
		test.Fatalf("the resolver never learned %s, which the probe ran on", fixture)
	}
	for path, encoded := range resolver.Recorded(roots[0].Types)[odd_slice_argument.Kind.Name] {
		kept.recorded[string(path)] = string(encoded)
	}
	names := map[types.Object]string{}
	scope := roots[0].Types.Scope()
	for _, name := range scope.Names() {
		if function, isFunction := scope.Lookup(name).(*types.Func); isFunction {
			for parameter := range function.Signature().Params().Variables() {
				names[parameter] = function.Name() + "." + parameter.Name()
			}
		}
	}
	for _, answer := range resolver.Answered() {
		if answer.Kind != odd_slice_argument.Kind || answer.Object.Pkg() != kept.home.Types {
			continue
		}
		parameter, isParameter := answer.Object.(*types.Var)
		name, isNamed := names[answer.Object]
		if !isParameter || !isNamed {
			test.Fatalf("the answer held about %s is about no parameter of a function the package declares",
				answer.Object.Name())
		}
		kept.answered[name], kept.parameters[name] = string(answer.Encoded), parameter
	}
	return kept
}

// requireFinal fails unless each mark the resolver holds is the one a question asked directly afterwards answers.
func (kept keptMarks) requireFinal(test *testing.T) {
	test.Helper()
	for name, held := range kept.answered {
		asked := odd_slice_argument.Ask(kept.resolver, kept.home, kept.parameters[name])
		if fmt.Sprint(asked) != held {
			test.Errorf("%s: asked directly the resolver answers marked %t, and the answer it held is %s", name, asked,
				held)
		}
	}
}

// Asked of Caller's slice alone, the resolver holds an answer of its own for every parameter of each function the
// judgment settled, none of which was asked of (#z9fze0t): Forward's values, marked, and its loose, not; and the
// unexported pairs' three, which no objectpath names, its values marked, its label and its rest not. Loose, which
// nothing reached, has none. What the run recorded for the store is the mark of Caller's slice alone, as a run that
// kept nothing records, and each answer held is the one a later question answers.
func TestEveryParameterAJudgmentSettlesHasAnAnswerOfItsOwn(test *testing.T) {
	test.Parallel()
	kept := askMarkOnce(test, "kept", "Caller")
	wantAnswered := map[string]string{"Caller.values": "true", "Forward.values": "true", "Forward.loose": "false",
		"pairs.label": "false", "pairs.values": "true", "pairs.rest": "false"}
	if !maps.Equal(kept.answered, wantAnswered) {
		test.Errorf("the resolver holds the marks %v, want %v", kept.answered, wantAnswered)
	}
	caller, isFunction := kept.home.Types.Scope().Lookup("Caller").(*types.Func)
	if !isFunction {
		test.Fatal("kept declares no function Caller")
	}
	path, pathError := objectpath.For(caller.Signature().Params().At(0))
	if pathError != nil {
		test.Fatal(pathError)
	}
	if wantRecorded := map[string]string{string(path): "true"}; !maps.Equal(kept.recorded, wantRecorded) {
		test.Errorf("the resolver recorded %v, want %v", kept.recorded, wantRecorded)
	}
	kept.requireFinal(test)
}

// What a judgment keeps of a cycle is what a later question answers (#z9fze0t). Outer and Inner pass a slice to each
// other, and a mark goes around their cycle before it settles, so no mark is handed to the resolver until the last
// pass has added nothing: asked of Inner's slice alone, the resolver holds Inner's, Outer's and Pairs's, all marked,
// and asked directly afterwards each answers as the resolver held it.
func TestWhatAJudgmentKeepsOfACycleIsWhatALaterQuestionAnswers(test *testing.T) {
	test.Parallel()
	kept := askMarkOnce(test, "demand/cycle", "Inner")
	wantAnswered := map[string]string{"Inner.values": "true", "Outer.values": "true", "Pairs.pairs": "true"}
	if !maps.Equal(kept.answered, wantAnswered) {
		test.Errorf("the resolver holds the marks %v, want %v", kept.answered, wantAnswered)
	}
	if recorded := slices.Sorted(maps.Keys(kept.recorded)); len(recorded) != 1 {
		test.Errorf("the resolver recorded marks at %q, want Inner's slice's alone", recorded)
	}
	kept.requireFinal(test)
}
