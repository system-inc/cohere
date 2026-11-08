package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"
	"honnef.co/go/tools/analysis/facts/generated"
	"honnef.co/go/tools/pattern"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_report"
)

// builtinNames are the names each expression is asked about: the universe's functions, types, constants and nil the
// ports' patterns name, and two the universe does not hold, the blank identifier and a made-up name, where the pattern
// compares two missing objects.
var builtinNames = []string{
	"append", "any", "cap", "clear", "copy", "delete", "error", "false", "iota", "len", "make", "min", "new", "nil",
	"string", "true", "_", "missing",
}

// builtinComparison is what one package's side by side run of IsBuiltin found.
type builtinComparison struct {
	// questions counts the expressions asked, each about every name of builtinNames.
	questions int
	// mismatches describes each question whose two answers differed.
	mismatches []string
	// met counts each label of requiredBuiltinLabels the run met.
	met map[string]int
}

// builtinAnalyzer asks both sides of every expression of a package, its result a builtinComparison. It requires
// staticcheck's generated pass, which PassContext does.
var builtinAnalyzer = &analysis.Analyzer{
	Name:       "comparebuiltin",
	Doc:        "asks staticcheck's pattern (Builtin name) and the port of every expression, and compares them",
	Requires:   []*analysis.Analyzer{generated.Analyzer},
	Run:        compareBuiltin,
	ResultType: reflect.TypeFor[*builtinComparison](),
}

// Both sides answer alike for every expression of the builtins fixture, which uses the universe's names plain and
// parenthesized and declares some of them again in blocks, and of the shadowed fixture, which declares len, cap, nil
// and string at package scope, about each name of builtinNames. staticcheck's side is its pattern package matching a
// parsed `(Builtin "name")` through code.Match, as every pattern that names a builtin asks it. The test fails unless
// each way an expression can be judged was met, so a fixture that stops holding one cannot pass by asking nothing.
func TestIsBuiltinMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	directory := analysistest.TestData()
	loaded, loadError := load.Packages(directory, []string{"builtins", "shadowed"}, []string{
		"GOPATH=" + directory, "GO111MODULE=off", "GOWORK=off",
	}, nil)
	if loadError != nil {
		test.Fatalf("loading builtins and shadowed: %v", loadError)
	}
	for _, loadedPackage := range loaded.Roots {
		if len(loadedPackage.Errors) > 0 {
			test.Fatalf("loading %s: %v", loadedPackage.PkgPath, loadedPackage.Errors)
		}
	}
	graph, analyzeError := checker.Analyze(
		[]*analysis.Analyzer{builtinAnalyzer}, loaded.Roots, &checker.Options{},
	)
	if analyzeError != nil {
		test.Fatalf("analyzing builtins and shadowed: %v", analyzeError)
	}
	questions := 0
	met := map[string]int{}
	for _, action := range graph.Roots {
		if action.Err != nil {
			test.Fatalf("running %s: %v", action, action.Err)
		}
		compared, isComparison := action.Result.(*builtinComparison)
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
	for _, label := range requiredBuiltinLabels() {
		test.Logf("%s: %d", label, met[label])
		if met[label] == 0 {
			test.Errorf("the fixtures never met %s, so the comparison proves nothing of it", label)
		}
	}
}

// compareBuiltin asks both sides of every expression of the pass's package about every name, recording each question
// they answer differently and the labels it met: the package, the expression's kind and the answer, and for an
// identifier asked its own name, whether the universe's object of that name was what it resolved to.
func compareBuiltin(pass *analysis.Pass) (any, error) {
	context, contextError := staticcheck_report.PassContext(pass)
	if contextError != nil {
		return nil, contextError
	}
	patterns := map[string]pattern.Pattern{}
	for _, name := range builtinNames {
		patterns[name] = pattern.MustParse(fmt.Sprintf("(Builtin %q)", name))
	}
	compared := &builtinComparison{met: map[string]int{}}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			kind := reflect.TypeOf(expression).Elem().Name()
			for _, name := range builtinNames {
				_, upstream := code.Match(pass, patterns[name], expression)
				port := staticcheck_code.IsBuiltin(context, expression, name)
				compared.questions++
				if upstream != port {
					compared.mismatches = append(compared.mismatches, fmt.Sprintf(
						"%s about %q at %s: staticcheck answered %t, the port %t",
						kind, name, pass.Fset.Position(expression.Pos()), upstream, port))
				}
				compared.met[fmt.Sprintf("%s, %s, %t", pass.Pkg.Name(), kind, upstream)]++
				if identifier, isIdentifier := expression.(*ast.Ident); isIdentifier && identifier.Name == name {
					compared.met[fmt.Sprintf("%s, its own name, %t", pass.Pkg.Name(), upstream)]++
				}
			}
			return true
		})
	}
	return compared, nil
}

// requiredBuiltinLabels are what the fixtures must meet for the comparison to prove each case: in builtins, an
// identifier and a parenthesized one each found a builtin and found none, and an identifier asked its own name found
// both the universe's object and one a block declares; in shadowed, an identifier asked its own name found none, the
// package's own declaration standing in the universe's place.
func requiredBuiltinLabels() []string {
	return []string{
		"builtins, Ident, true", "builtins, Ident, false",
		"builtins, ParenExpr, true", "builtins, ParenExpr, false",
		"builtins, its own name, true", "builtins, its own name, false",
		"shadowed, its own name, false",
	}
}
