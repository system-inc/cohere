package staticcheck_call

import (
	"cmp"
	"fmt"
	"go/constant"
	"os"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// markEverything is a Rule that marks each argument of a call to name with its index, its type and, when it is a
// constant integer, its value, and marks the call itself, so every way the kit places a mark is seen.
func markEverything(name string) Rule {
	return func(call *Call) {
		for index, argument := range call.Arguments {
			message := fmt.Sprintf("%s argument %d: %s", name, index, argument.Value.Type())
			if extracted := ExtractConstantOfKind(argument.Value, constant.Int); extracted != nil {
				message += " = " + extracted.Value.ExactString()
			}
			argument.Invalid(message)
		}
		call.Invalid(name + " call")
	}
}

// functionCheckAnalyzer runs check as a plain analyzer named name over every function staticcheck_ir.Functions reads,
// with the context staticcheck_report.PassContext builds around pass.Report, so the test runs the check outside the
// engine's dispatcher and reads its findings straight off the analyzer.
func functionCheckAnalyzer(name string, doc string, check rule.FunctionCheck) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name:     name,
		Doc:      doc,
		Requires: []*analysis.Analyzer{staticcheck_ir.Analyzer, generated.Analyzer},
		Run: func(pass *analysis.Pass) (any, error) {
			functions, readError := staticcheck_ir.Functions(pass)
			if readError != nil {
				return nil, readError
			}
			context, contextError := staticcheck_report.PassContext(pass)
			if contextError != nil {
				return nil, contextError
			}
			for _, function := range functions {
				check(context, function)
			}
			return nil, nil
		},
	}
}

// marked is one mark as the test reads it: its message and the source text its range covers.
type marked struct {
	message string
	text    string
}

// Each mark lands where staticcheck's callcheck puts it, over the same range: an argument's mark over its expression,
// the receiver of a method left out of the arguments, an argument converted to an interface judged as what was
// converted, both values of f(g()) over the one argument there is, a variadic call given nothing over the call, and a
// call's own mark over the call expression, or over the defer or go statement around it. The call of a function
// ranged over has no syntax, so its marks, where the original panics, are dropped. A phi of two constants is no
// constant, and a constant of another kind than asked for is none either.
func TestFunctionCheckMarksWhereCallcheckReports(test *testing.T) {
	test.Parallel()
	rules := map[string]Rule{}
	for _, name := range []string{
		"calls.single", "(calls.receiver).method", "(*calls.receiver).pointerMethod", "calls.takesPair",
		"calls.variadic", "calls.sequence",
	} {
		rules[name] = markEverything(name)
	}
	analyzer := functionCheckAnalyzer("calls", "marks every argument of every ruled call", FunctionCheck(rules))
	expected := []marked{
		{"calls.single argument 0: int = 3", "1 + 2"},
		{"calls.single call", "single(1 + 2)"},
		{"(calls.receiver).method argument 0: int = 3", "3"},
		{"(calls.receiver).method argument 1: string", `"four"`},
		{"(calls.receiver).method call", `receiver{}.method(3, "four")`},
		{"(*calls.receiver).pointerMethod argument 0: int = 5", "5"},
		{"(*calls.receiver).pointerMethod call", "(&receiver{}).pointerMethod(5)"},
		{"calls.takesPair argument 0: int", "pair()"},
		{"calls.takesPair argument 1: string", "pair()"},
		{"calls.takesPair call", "takesPair(pair())"},
		{"calls.single argument 0: int = 6", "6"},
		{"calls.single call", "defer single(6)"},
		{"calls.single argument 0: int = 7", "7"},
		{"calls.single call", "go single(7)"},
		{"calls.variadic argument 0: []int", "variadic()"},
		{"calls.variadic call", "variadic()"},
		{"calls.variadic argument 0: []int", "8"},
		{"calls.variadic call", "variadic(8, 9)"},
		{"calls.single argument 0: int", "number"},
		{"calls.single call", "single(number)"},
	}
	actual := marksOf(test, analyzer)
	compare := func(first marked, second marked) int {
		return cmp.Or(cmp.Compare(first.message, second.message), cmp.Compare(first.text, second.text))
	}
	slices.SortFunc(expected, compare)
	slices.SortFunc(actual, compare)
	if !slices.Equal(actual, expected) {
		test.Errorf("the kit marked\n%v\nwhere callcheck marks\n%v", actual, expected)
	}
}

// marksOf runs analyzer over the calls fixture and returns each finding as the text its range covers.
func marksOf(test *testing.T, analyzer *analysis.Analyzer) []marked {
	test.Helper()
	directory := analysistest.TestData()
	// The fixture is a tree laid out by import path, loaded as analysistest loads one.
	loaded, loadError := load.Packages(directory, []string{"calls"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading the fixture: %v", loadError)
	}
	graph, analyzeError := checker.Analyze([]*analysis.Analyzer{analyzer}, loaded.Roots, &checker.Options{})
	if analyzeError != nil {
		test.Fatalf("analyzing the fixture: %v", analyzeError)
	}
	marks := []marked{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		for _, diagnostic := range action.Diagnostics {
			position := action.Package.Fset.Position(diagnostic.Pos)
			end := action.Package.Fset.Position(diagnostic.End)
			if !end.IsValid() || end.Filename != position.Filename {
				test.Errorf("%s: %q ends at %s, not in its own file", position, diagnostic.Message, end)
				continue
			}
			content, readError := os.ReadFile(position.Filename)
			if readError != nil {
				test.Fatal(readError)
			}
			marks = append(marks, marked{
				message: diagnostic.Message,
				text:    string(content[position.Offset:end.Offset]),
			})
		}
	}
	return marks
}
