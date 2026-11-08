package staticcheck_code_test

import (
	"fmt"
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides judge alike whether every expression of the selectors and constants fixtures refers to each object
// the package defines or uses, and to no object at all. The test fails unless both answers were met for an object
// and for nil, so a fixture that stops holding one cannot pass by asking nothing of it.
func TestRefersToMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "comparerefersto", []fixtureLoad{pathFixture("selectors"), pathFixture("constants")},
		compareRefersTo)
	requireMet(test, met, []string{
		"RefersTo an object, true", "RefersTo an object, false", "RefersTo nil, true", "RefersTo nil, false",
	})
}

// compareRefersTo asks both sides of every expression of the pass's package whether it refers to each object the
// package defines or uses, and to nil.
func compareRefersTo(pass *analysis.Pass, context *rule.Context) []question {
	objects := []types.Object{nil}
	seen := map[types.Object]bool{nil: true}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			identifier, isIdentifier := node.(*ast.Ident)
			if !isIdentifier {
				return true
			}
			object := pass.TypesInfo.ObjectOf(identifier)
			if !seen[object] {
				seen[object] = true
				objects = append(objects, object)
			}
			return true
		})
	}
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			position := pass.Fset.Position(expression.Pos())
			for _, object := range objects {
				asked := ask(fmt.Sprintf("RefersTo %v", object), position, "",
					func() bool { return code.RefersTo(pass, expression, object) },
					func() bool { return staticcheck_code.RefersTo(context, expression, object) })
				asked.label = "RefersTo an object, " + asked.upstream
				if object == nil {
					asked.label = "RefersTo nil, " + asked.upstream
				}
				questions = append(questions, asked)
			}
			return true
		})
	}
	return questions
}
