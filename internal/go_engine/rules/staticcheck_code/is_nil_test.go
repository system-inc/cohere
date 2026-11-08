package staticcheck_code_test

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides judge alike whether every expression of the constants fixture is nil. The test fails unless nil bare
// and parenthesized, and expressions of a nil value that are not nil itself, were met, so a fixture that stops
// holding one cannot pass by asking nothing of it.
func TestIsNilMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareisnil", []fixtureLoad{pathFixture("constants")}, compareIsNil)
	requireMet(test, met, []string{"IsNil of Ident, true", "IsNil of ParenExpr, true", "IsNil of Ident, false",
		"IsNil of CallExpr, false"})
}

// compareIsNil asks both sides of every expression of the pass's package whether it is nil.
func compareIsNil(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			asked := ask("IsNil", pass.Fset.Position(expression.Pos()), "",
				func() bool { return code.IsNil(pass, expression) },
				func() bool { return staticcheck_code.IsNil(context, expression) })
			asked.label = "IsNil of " + kindOf(expression) + ", " + asked.upstream
			questions = append(questions, asked)
			return true
		})
	}
	return questions
}
