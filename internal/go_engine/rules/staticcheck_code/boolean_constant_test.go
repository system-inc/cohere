//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_code_test

import (
	"go/ast"
	"go/constant"
	"go/types"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// Both sides judge alike whether every expression of the constants fixture is a boolean constant, and read the same
// value of every identifier naming a constant whose value is a boolean, of whatever type. Asked of any other
// expression, BoolConst panics on both sides, which is all that is compared there, since each says why in its own
// words. The test fails unless a constant of bool, an untyped one, one of a named boolean type and one of an alias
// were met, and each answer of each helper, so a fixture that stops holding one cannot pass by asking nothing of it.
func TestBoolConstMatchesStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareboolconst", []fixtureLoad{pathFixture("constants")}, compareBoolConst)
	requireMet(test, met, []string{
		"IsBoolConst, true", "IsBoolConst, false",
		"IsBoolConst of a constant of a named type, false", "IsBoolConst of a constant of an alias, false",
		"BoolConst, true", "BoolConst, false", "BoolConst of a constant of a named type, true",
		"BoolConst, whether it panics, true",
	})
}

// compareBoolConst asks both sides of every expression of the pass's package whether it is a boolean constant, and
// its value or whether asking for one panics.
func compareBoolConst(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			position := pass.Fset.Position(expression.Pos())
			shape := ""
			named, namesConstant := constantNamed(pass, expression)
			if namesConstant {
				switch named.Type().(type) {
				case *types.Named:
					shape = " of a constant of a named type"
				case *types.Alias:
					shape = " of a constant of an alias"
				}
			}
			asked := ask("IsBoolConst", position, "",
				func() bool { return code.IsBoolConst(pass, expression) },
				func() bool { return staticcheck_code.IsBoolConst(context, expression) })
			asked.label = "IsBoolConst" + shape + ", " + asked.upstream
			questions = append(questions, asked)
			if namesConstant && named.Val().Kind() == constant.Bool {
				asked := ask("BoolConst", position, "",
					func() bool { return code.BoolConst(pass, expression) },
					func() bool { return staticcheck_code.BoolConst(context, expression) })
				asked.label = "BoolConst" + shape + ", " + asked.upstream
				questions = append(questions, asked)
				return true
			}
			asked = ask("BoolConst, whether it panics", position, "",
				func() bool { return panics(func() { code.BoolConst(pass, expression) }) },
				func() bool { return panics(func() { staticcheck_code.BoolConst(context, expression) }) })
			asked.label = "BoolConst, whether it panics, " + asked.upstream
			questions = append(questions, asked)
			return true
		})
	}
	return questions
}

// constantNamed is the constant expression names, when it is an identifier naming one.
func constantNamed(pass *analysis.Pass, expression ast.Expr) (*types.Const, bool) {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier {
		return nil, false
	}
	named, isConstant := pass.TypesInfo.ObjectOf(identifier).(*types.Const)
	return named, isConstant
}

// panics is whether calling panics.
func panics(calling func()) (panicked bool) {
	defer func() {
		panicked = recover() != nil
	}()
	calling()
	return false
}
