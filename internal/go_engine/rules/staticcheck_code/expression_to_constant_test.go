package staticcheck_code_test

import (
	"go/ast"
	"go/constant"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/code"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_code"
)

// integerAnswer is what ExprToInt answered, spelled to compare.
type integerAnswer struct {
	value int64
	exact bool
}

// stringAnswer is what ExprToString answered, spelled to compare.
type stringAnswer struct {
	value    string
	isString bool
}

// Both sides read the same integer and the same string of every expression of the constants fixture. The test fails
// unless an integer that fits an int64, one that does not, a string, a constant of another kind and an expression of
// no constant value were each met by each helper, so a fixture that stops holding one cannot pass by asking nothing
// of it.
func TestExprToIntAndExprToStringMatchStaticcheck(test *testing.T) {
	test.Parallel()
	met := compareHelper(test, "compareexprtoconstant", []fixtureLoad{pathFixture("constants")},
		compareExprToConstant)
	requireMet(test, met, []string{
		"ExprToInt of an integer, exact", "ExprToInt of an integer, inexact", "ExprToInt of another kind",
		"ExprToInt of no constant",
		"ExprToString of a string", "ExprToString of another kind", "ExprToString of no constant",
	})
}

// compareExprToConstant asks both sides the integer and the string of every expression of the pass's package.
func compareExprToConstant(pass *analysis.Pass, context *rule.Context) []question {
	questions := []question{}
	for _, file := range pass.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			expression, isExpression := node.(ast.Expr)
			if !isExpression {
				return true
			}
			position := pass.Fset.Position(expression.Pos())
			value := pass.TypesInfo.Types[expression].Value
			integer := ask("ExprToInt", position, "", func() integerAnswer {
				answered, exact := code.ExprToInt(pass, expression)
				return integerAnswer{answered, exact}
			}, func() integerAnswer {
				answered, exact := staticcheck_code.ExprToInt(context, expression)
				return integerAnswer{answered, exact}
			})
			text := ask("ExprToString", position, "", func() stringAnswer {
				answered, isString := code.ExprToString(pass, expression)
				return stringAnswer{answered, isString}
			}, func() stringAnswer {
				answered, isString := staticcheck_code.ExprToString(context, expression)
				return stringAnswer{answered, isString}
			})
			switch {
			case value == nil:
				integer.label = "ExprToInt of no constant"
				text.label = "ExprToString of no constant"
			case value.Kind() == constant.Int:
				_, exact := constant.Int64Val(value)
				integer.label = map[bool]string{
					true: "ExprToInt of an integer, exact", false: "ExprToInt of an integer, inexact",
				}[exact]
				text.label = "ExprToString of another kind"
			case value.Kind() == constant.String:
				integer.label = "ExprToInt of another kind"
				text.label = "ExprToString of a string"
			default:
				integer.label = "ExprToInt of another kind"
				text.label = "ExprToString of another kind"
			}
			questions = append(questions, integer, text)
			return true
		})
	}
	return questions
}
