package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// integerLiteralComparison is what one package's side by side run of IntegerLiteral found.
type integerLiteralComparison struct {
	// questions counts the nodes asked.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met counts each label of requiredIntegerLiteralLabels the run met.
	met map[string]int
}

// integerLiteralAnalyzer asks both sides of every node of a package, its result an integerLiteralComparison. It
// requires staticcheck's generated pass, which PassContext does.
var integerLiteralAnalyzer = &analysis.Analyzer{
	Name:       "compareintegerliteral",
	Doc:        "asks staticcheck's code.IntegerLiteral and the port of every node, and compares them",
	Requires:   []*analysis.Analyzer{generated.Analyzer},
	Run:        compareIntegerLiteral,
	ResultType: reflect.TypeFor[*integerLiteralComparison](),
}

// Both sides answer alike for every expression of the integers fixture, which holds integer literals in every base
// and spelling, under signs and parentheses, and the constants that only look like one: named constants,
// conversions, other operators, character and float literals. Where both find a literal, both give the same type and
// value. staticcheck's is also asked of every node that is no expression, which the port does not take, and must
// find none, so the statements its matcher looks through never hold one in code that type-checks. The test fails
// unless each kind of expression that could be a literal was answered both ways, so a fixture that stops holding one
// cannot pass by asking nothing.
func TestIntegerLiteralMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"integers"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading integers: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{integerLiteralAnalyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing integers: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*integerLiteralComparison)
		if !isComparison {
			test.Fatalf("running %s: its result is %T, where a comparison was expected", action, action.Result)
		}
		questions += compared.questions
		for _, mismatch := range compared.mismatches {
			test.Error(mismatch)
		}
		for label, count := range compared.met {
			met[label] += count
		}
	}
	test.Logf("%d questions asked of each side", questions)
	for _, label := range requiredIntegerLiteralLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// compareIntegerLiteral asks both sides of every expression of the pass's package, and staticcheck's of every other
// node, recording each question they answer differently and the labels it met.
func compareIntegerLiteral(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	compared := &integerLiteralComparison{met: map[string]int{}}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if node == nil {
				return true
			}
			upstream, upstreamIsLiteral := code.IntegerLiteral(pass, node)
			compared.questions++
			kind := reflect.TypeOf(node).Elem().Name()
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				if upstreamIsLiteral {
					compared.mismatches = append(compared.mismatches, fmt.Sprintf(
						"%s at %s: staticcheck found an integer literal in a node that is no expression", kind,
						pass.Fset.Position(node.Pos())))
				}
				return true
			}
			port, portIsLiteral := staticcheck_code.IntegerLiteral(context, expression)
			compared.met[fmt.Sprintf("%s, %t", kind, upstreamIsLiteral)]++
			// Both read the same recorded entry, so the same mode, the same type and the same constant.
			if upstreamIsLiteral != portIsLiteral || upstream != port {
				compared.mismatches = append(compared.mismatches, fmt.Sprintf(
					"%s at %s: staticcheck answered %t with %v, the port %t with %v", kind,
					pass.Fset.Position(expression.Pos()), upstreamIsLiteral, upstream, portIsLiteral, port))
			}
			return true
		})
	}
	return compared, nil
}

// requiredIntegerLiteralLabels are what the fixture must meet for the comparison to prove each case: a basic literal,
// a unary expression and a parenthesized one each found a literal and found none, and a call, an identifier and a
// binary expression each found none.
func requiredIntegerLiteralLabels() []string {
	return []string{
		"BasicLit, true", "BasicLit, false",
		"UnaryExpr, true", "UnaryExpr, false",
		"ParenExpr, true", "ParenExpr, false",
		"CallExpr, false", "Ident, false", "BinaryExpr, false",
	}
}
