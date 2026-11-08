package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/analysis/facts/purity"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// sideEffectsComparison is what one package's side by side run of MayHaveSideEffects found.
type sideEffectsComparison struct {
	// questions counts the expressions asked, each under every judgment of purity.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met counts each label of requiredSideEffectsLabels the run met.
	met map[string]int
}

// purityJudgment is one way of judging purity, spelled for each side: staticcheck's map, and the port's predicate.
type purityJudgment struct {
	name     string
	upstream purity.Result
	port     func(function *types.Func) bool
}

// sideEffectsAnalyzer asks both sides of every expression of a package, its result a sideEffectsComparison. It
// requires staticcheck's purity pass, whose result the judgments read, and its generated pass, which PassContext does.
var sideEffectsAnalyzer = &analysis.Analyzer{
	Name:       "comparesideeffects",
	Doc:        "asks staticcheck's code.MayHaveSideEffects and the port of every expression, and compares them",
	Requires:   []*analysis.Analyzer{purity.Analyzer, generated.Analyzer},
	Run:        compareSideEffects,
	ResultType: reflect.TypeFor[*sideEffectsComparison](),
}

// Both sides answer alike for every expression of the expressions fixture, under each of three judgments of purity:
// none at all, where every call may have side effects; a judgment that finds nothing pure, where len and cap still
// have none; and staticcheck's purity pass, which finds the fixture's pure function. The test fails unless every kind
// of expression go/ast declares was asked, and calls and unary expressions answered both ways, so a fixture that
// stops holding one cannot pass by asking nothing. A bad expression, which no package that type-checks holds, is
// asked directly.
func TestMayHaveSideEffectsMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"expressions"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading expressions: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{sideEffectsAnalyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing expressions: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*sideEffectsComparison)
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
	for _, label := range requiredSideEffectsLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixture never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// A bad expression, which the parser leaves where it could not read one, may have side effects for both sides. It
// needs no package, since neither side reads the type information for it.
func TestMayHaveSideEffectsOfABadExpressionMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	bad := &ast.BadExpr{}
	upstream := code.MayHaveSideEffects(&analysis.Pass{}, bad, nil)
	port := staticcheck_code.MayHaveSideEffects(&rule.Context{}, bad, nil)
	if !upstream || upstream != port {
		test.Errorf("of a bad expression staticcheck answered %t and the port %t, where both answer true", upstream,
			port)
	}
}

// compareSideEffects asks both sides of every expression of the pass's package under every judgment of purity,
// recording each question they answer differently and the labels it met.
func compareSideEffects(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	pure, isPurity := pass.ResultOf[purity.Analyzer].(purity.Result)
	if !isPurity {
		return nil, fmt.Errorf("the purity pass's result is %T", pass.ResultOf[purity.Analyzer])
	}
	judgments := []purityJudgment{
		{"no judgment", nil, nil},
		{"nothing pure", purity.Result{}, func(*types.Func) bool { return false }},
		{"the purity pass", pure, func(function *types.Func) bool {
			_, isPure := pure[function]
			return isPure
		}},
	}
	compared := &sideEffectsComparison{met: map[string]int{}}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			kind := reflect.TypeOf(expression).Elem().Name()
			compared.met[kind]++
			for _, judgment := range judgments {
				upstream := code.MayHaveSideEffects(pass, expression, judgment.upstream)
				port := staticcheck_code.MayHaveSideEffects(context, expression, judgment.port)
				compared.questions++
				if upstream != port {
					compared.mismatches = append(compared.mismatches, fmt.Sprintf(
						"%s under %s at %s: staticcheck answered %t, the port %t",
						kind, judgment.name, pass.Fset.Position(expression.Pos()), upstream, port))
				}
				compared.met[fmt.Sprintf("%s under %s, %t", kind, judgment.name, upstream)]++
			}
			return true
		})
	}
	return compared, nil
}

// requiredSideEffectsLabels are what the fixture must meet for the comparison to prove each case: every kind of
// expression go/ast declares but the bad one, a call answered both ways under each judgment that finds anything
// pure, a call under no judgment answered true, and a unary expression answered both ways.
func requiredSideEffectsLabels() []string {
	return []string{
		"ArrayType", "BasicLit", "BinaryExpr", "CallExpr", "ChanType", "CompositeLit", "Ellipsis", "FuncLit",
		"FuncType", "Ident", "IndexExpr", "IndexListExpr", "InterfaceType", "KeyValueExpr", "MapType", "ParenExpr",
		"SelectorExpr", "SliceExpr", "StarExpr", "StructType", "TypeAssertExpr", "UnaryExpr",
		"CallExpr under no judgment, true",
		"CallExpr under nothing pure, true", "CallExpr under nothing pure, false",
		"CallExpr under the purity pass, true", "CallExpr under the purity pass, false",
		"UnaryExpr under the purity pass, true", "UnaryExpr under the purity pass, false",
		"SliceExpr under the purity pass, true", "SliceExpr under the purity pass, false",
		"CompositeLit under the purity pass, true", "CompositeLit under the purity pass, false",
	}
}
