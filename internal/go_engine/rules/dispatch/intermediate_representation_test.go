package dispatch

import (
	"fmt"
	"go/ast"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
)

// The walk over the engine's own intermediate representation calls each listener for what it declares and nothing
// else, in order: for each function in the order SourceFunctions reads them, every listener's Function,
// in the order they were registered, then each instruction of a listed kind in block order, then FunctionExit; and
// PackageExit once, after every function. The expected log is built from a probe that reads the same functions in
// the same checker.
func TestTheOwnWalkCallsEachListenerForWhatItDeclaresInOrder(test *testing.T) {
	test.Parallel()
	heard := []string{}
	dispatcher := NewIntermediateRepresentation()
	listening := dispatcher.RuleAnalyzer(rule.Namespace+"listening", "listening", "records each call",
		&rule.Listener{
			IntermediateRepresentation: &rule.IntermediateRepresentationListener{
				Function: func(_ *rule.Context, function *intermediate_representation.Function) {
					heard = append(heard, "function "+function.Name())
				},
				Instructions: []intermediate_representation.Instruction{
					(*intermediate_representation.Call)(nil), (*intermediate_representation.Return)(nil),
				},
				Instruction: func(_ *rule.Context, instruction intermediate_representation.Instruction) {
					heard = append(heard, fmt.Sprintf("instruction %T in %s", instruction, instruction.Parent().Name()))
				},
				FunctionExit: func(_ *rule.Context, function *intermediate_representation.Function) {
					heard = append(heard, "exit "+function.Name())
				},
			},
			PackageExit: func(*rule.Context) {
				heard = append(heard, "package")
			},
		})
	alongside := dispatcher.RuleAnalyzer(rule.Namespace+"alongside", "alongside", "records each function",
		&rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
			Function: func(_ *rule.Context, function *intermediate_representation.Function) {
				heard = append(heard, "alongside function "+function.Name())
			},
		}})
	functions := []*intermediate_representation.Function{}
	probe := &analysis.Analyzer{
		Name:     "own_probe",
		Doc:      "reads the functions the dispatcher walks",
		Requires: []*analysis.Analyzer{build_intermediate_representation.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			built, readError := build_intermediate_representation.ResultOf(pass)
			if readError != nil {
				return nil, readError
			}
			functions = slices.Collect(built.SourceFunctions(nil, ""))
			return nil, nil
		},
	}
	graph := analyzeFixture(test, "dispatched", listening, alongside, probe)
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
	}

	expected := []string{}
	unlisted := 0
	for _, function := range functions {
		expected = append(expected, "function "+function.Name(), "alongside function "+function.Name())
		for _, block := range function.Blocks {
			for _, instruction := range block.Instructions {
				switch instruction.(type) {
				case *intermediate_representation.Call, *intermediate_representation.Return:
					expected = append(expected, fmt.Sprintf("instruction %T in %s", instruction, function.Name()))
				default:
					unlisted++
				}
			}
		}
		expected = append(expected, "exit "+function.Name())
	}
	expected = append(expected, "package")
	if len(functions) < 4 || unlisted == 0 {
		test.Fatalf("the fixture read as %d functions with %d instructions of kinds no listener lists, too few to "+
			"prove the walk or its filter", len(functions), unlisted)
	}
	if !slices.Equal(heard, expected) {
		test.Errorf("the listeners heard\n%s\nwhere they declared\n%s", strings.Join(heard, "\n"),
			strings.Join(expected, "\n"))
	}
}

// A listener of the engine's own walk that panics is a crash under its own rule on the package, spelled as the engine
// spells an analyzer's panic, with none of its findings there, the one it reported before panicking included, and it
// is called no more on that package, its PackageExit included. The listener beside it still reports every function.
func TestAPanickingOwnListenerCrashesItsOwnRuleAlone(test *testing.T) {
	test.Parallel()
	dispatcher := NewIntermediateRepresentation()
	crashingCalls := 0
	crashingExited := false
	crashing := dispatcher.RuleAnalyzer(rule.Namespace+"crashing", "crashing",
		"reports, then panics on the second function", &rule.Listener{
			IntermediateRepresentation: &rule.IntermediateRepresentationListener{
				Function: func(context *rule.Context, _ *intermediate_representation.Function) {
					crashingCalls++
					context.Report(analysis.Diagnostic{Pos: context.Files[0].Name.Pos(), Message: "before the panic"})
					if crashingCalls == 2 {
						panic("planted panic")
					}
				},
			},
			PackageExit: func(*rule.Context) {
				crashingExited = true
			},
		})
	reported := 0
	steady := dispatcher.RuleAnalyzer(rule.Namespace+"steady", "steady", "reports every function",
		&rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
			Function: func(context *rule.Context, function *intermediate_representation.Function) {
				reported++
				context.Report(analysis.Diagnostic{Pos: context.Files[0].Name.Pos(), Message: function.Name()})
			},
		}})
	var outcomes Result
	resultProbe := &analysis.Analyzer{
		Name:     "result_probe",
		Doc:      "reads the dispatcher's result",
		Requires: []*analysis.Analyzer{dispatcher.Analyzer()},
		Run: func(pass *analysis.Pass) (any, error) {
			result, isResult := pass.ResultOf[dispatcher.Analyzer()].(Result)
			if !isResult {
				return nil, fmt.Errorf("the dispatcher's result is %T", pass.ResultOf[dispatcher.Analyzer()])
			}
			outcomes = result
			return nil, nil
		},
	}
	graph := analyzeFixture(test, "dispatched", crashing, steady, resultProbe)
	if outcome := outcomes[rule.Namespace+"crashing"]; outcome.Crash == nil || len(outcome.Diagnostics) != 0 {
		test.Errorf("the dispatcher's outcome for the panicking rule holds the crash %v and %d findings, where the "+
			"crash alone was expected", outcome.Crash, len(outcome.Diagnostics))
	}
	for _, action := range graph.Roots {
		switch action.Analyzer {
		case crashing:
			if action.Err == nil || !strings.HasPrefix(action.Err.Error(),
				"running analyzer crashing on dispatched: it panicked with planted panic, at ") ||
				len(action.Diagnostics) != 0 {
				test.Errorf("the panicking rule ended with %v and %d findings, where a crash naming the panic and "+
					"no finding were expected", action.Err, len(action.Diagnostics))
			}
		case steady:
			if action.Err != nil || len(action.Diagnostics) != reported || reported < 4 {
				test.Errorf("the rule beside the panic ended with %v and %d findings of its %d reports, where every "+
					"function's was expected", action.Err, len(action.Diagnostics), reported)
			}
		}
	}
	if crashingCalls != 2 || crashingExited {
		test.Errorf("the panicking listener was called %d times and exited the package %t, where it should stop at "+
			"its panic, the second call", crashingCalls, crashingExited)
	}
}

// A listener that reads ValueForExpression declares it, and its rule's analyzer then requires the representation and
// its ExpressionReader mark, so the package records the expression map wherever the rule runs. Under the engine's
// Dependents, that listener finds the fixture's expressions mapped, and a listener that doesn't declare it, run
// alone, finds none: nothing in the run reads the map, so the package never records it.
func TestAReaderOfTheExpressionMapFindsItUnderTheEnginesDependents(test *testing.T) {
	test.Parallel()
	mapped := func(counted *int) *rule.Listener {
		return &rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
			Function: func(_ *rule.Context, function *intermediate_representation.Function) {
				if function.Syntax() == nil || function.Parent() != nil {
					return
				}
				ast.Inspect(function.Syntax(), func(node ast.Node) bool {
					if expression, isExpression := node.(ast.Expr); isExpression {
						if value, _ := function.ValueForExpression(expression); value != nil {
							*counted++
						}
					}
					return true
				})
			},
		}}
	}
	readerCount, otherCount := 0, 0
	readerListener := mapped(&readerCount)
	readerListener.IntermediateRepresentation.ReadsValueForExpression = true
	reader := NewIntermediateRepresentation().RuleAnalyzer(rule.Namespace+"reader", "reader",
		"counts the mapped expressions", readerListener)
	other := NewIntermediateRepresentation().RuleAnalyzer(rule.Namespace+"other", "other",
		"counts the mapped expressions without declaring it reads them", mapped(&otherCount))
	if !slices.Contains(reader.Requires, build_intermediate_representation.ExpressionReader) ||
		!slices.Contains(reader.Requires, build_intermediate_representation.Analyzer) {
		test.Errorf(
			"the reader's analyzer requires %v, which leaves out the representation or its mark",
			reader.Requires,
		)
	}
	if slices.Contains(other.Requires, build_intermediate_representation.ExpressionReader) {
		test.Errorf("an analyzer whose listener reads no expression requires the mark: %v", other.Requires)
	}
	options := &checker.Options{Dependents: rule.Dependents}
	for _, analyzer := range []*analysis.Analyzer{reader, other} {
		if _, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, loadFixture(test, "dispatched"),
			options); analyzeError != nil {
			test.Fatalf("analyzing the fixture with %s: %v", analyzer.Name, analyzeError)
		}
	}
	if readerCount == 0 || otherCount != 0 {
		test.Errorf("the reader of the map found %d mapped expressions and the other listener %d, where the reader "+
			"some and the other none were expected", readerCount, otherCount)
	}
}

// Each dispatcher refuses, as it is built, a listener of a walk it doesn't make, so a listener lands on exactly one of
// them, and one that declares neither walk lands on none.
func TestEachDispatcherRefusesAnotherWalksListener(test *testing.T) {
	test.Parallel()
	noWalkListener := &rule.Listener{PackageExit: func(*rule.Context) {
		// Never called: registering refuses it first.
	}}
	ownListener := &rule.Listener{IntermediateRepresentation: &rule.IntermediateRepresentationListener{
		Function: func(*rule.Context, *intermediate_representation.Function) {
			// Never called: registering refuses it first.
		},
	}}
	syntaxListener := &rule.Listener{FileEnter: func(*rule.Context, *ast.File) {
		// Never called: registering refuses it first.
	}}
	cases := []struct {
		name     string
		register func()
	}{
		{"neither walk's on IntermediateRepresentation", func() {
			NewIntermediateRepresentation().RuleAnalyzer("neither", "neither", "", noWalkListener)
		}},
		{"neither walk's on Syntax", func() { NewSyntax().RuleAnalyzer("neither", "neither", "", noWalkListener) }},
		{"syntax on IntermediateRepresentation", func() {
			NewIntermediateRepresentation().RuleAnalyzer("syntax", "syntax", "", syntaxListener)
		}},
		{"the engine's own on Syntax", func() { NewSyntax().RuleAnalyzer("own", "own", "", ownListener) }},
	}
	for _, testCase := range cases {
		panicked := func() (recovered any) {
			defer func() { recovered = recover() }()
			testCase.register()
			return nil
		}()
		if panicked == nil {
			test.Errorf("registering %s was accepted", testCase.name)
		}
	}
}
