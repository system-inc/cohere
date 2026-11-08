package staticcheck_code

import (
	"go/ast"
	"go/constant"
	"go/token"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsIntegerLiteral is whether node is written as an integer literal whose value equals value, such as -1 asked
// against constant.MakeInt64(-1). It is staticcheck's code.IsIntegerLiteral, from honnef.co/go/tools@v0.8.1
// analysis/code/code.go, a different function from IntegerLiteral beside it: IntegerLiteral, the pattern language's
// IntegerLiteral, answers whether an expression is an integer literal and with what type and value, and this asks it,
// then compares that value with value as staticcheck's does, by constant.Compare.
//
// constant.Compare compares a value no integer converts to, a string or a boolean, with itself, so such a value
// equals every integer literal here, as in staticcheck's. A node that is no expression is no literal: staticcheck's
// matcher looks through the statements that wrap an expression, but no statement holding a literal alone
// type-checks.
func IsIntegerLiteral(context *rule.Context, node ast.Node, value constant.Value) bool {
	expression, isExpression := node.(ast.Expr)
	if !isExpression {
		return false
	}
	typeAndValue, isLiteral := IntegerLiteral(context, expression)
	if !isLiteral {
		return false
	}
	return constant.Compare(typeAndValue.Value, token.EQL, value)
}
