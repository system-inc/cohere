package staticcheck_code_test

import (
	"go/ast"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides name every selector of the selectors and calls fixtures alike. The test fails unless a qualified
// identifier, a field direct, promoted and through a pointer, a method value, a method expression and a method of
// an interface were each met, so a fixture that stops holding one cannot pass by asking nothing of it.
func TestSelectorNameMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareselectorname", []fixtureLoad{pathFixture("selectors"), pathFixture("calls")},
		compareSelectorName)
	requireMet(test, met, []string{
		"a qualified identifier", "a field", "a promoted field", "a field through a pointer", "a method value",
		"a promoted method value", "a method expression", "a method of an interface",
	})
}

// compareSelectorName asks both sides the name of every selector of the pass's package, labeling each by what it
// selects.
func compareSelectorName(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			selector, isSelector := node.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			questions = append(questions, ask("SelectorName", pass.Fset.Position(selector.Pos()),
				selectionLabel(pass.TypesInfo.Selections[selector]),
				func() string { return code.SelectorName(pass, selector) },
				func() string { return staticcheck_code.SelectorName(context, selector) }))
			return true
		})
	}
	return questions
}

// selectionLabel is what selection selects, and "a qualified identifier" where the type checker recorded none.
func selectionLabel(selection *types.Selection) string {
	if selection == nil {
		return "a qualified identifier"
	}
	_, throughPointer := selection.Recv().Underlying().(*types.Pointer)
	_, ofInterface := selection.Recv().Underlying().(*types.Interface)
	promoted := len(selection.Index()) > 1
	switch {
	case selection.Kind() == types.FieldVal && promoted:
		return "a promoted field"
	case selection.Kind() == types.FieldVal && throughPointer:
		return "a field through a pointer"
	case selection.Kind() == types.FieldVal:
		return "a field"
	case selection.Kind() == types.MethodExpr:
		return "a method expression"
	case ofInterface:
		return "a method of an interface"
	case promoted:
		return "a promoted method value"
	default:
		return "a method value"
	}
}
