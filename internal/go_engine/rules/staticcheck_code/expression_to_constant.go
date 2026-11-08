package staticcheck_code

import (
	"go/ast"
	"go/constant"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// ExprToInt is the value of expression, a constant integer, and whether it is one that fits in an int64. It is
// staticcheck's code.ExprToInt, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// An integer constant too large for an int64 answers false with the value constant.Int64Val gives it, as
// staticcheck's does. A constant of another kind is no integer here.
func ExprToInt(context *rule.Context, expression ast.Expr) (int64, bool) {
	value := context.TypesInfo.Types[expression].Value
	if value == nil {
		return 0, false
	}
	if value.Kind() != constant.Int {
		return 0, false
	}
	return constant.Int64Val(value)
}

// ExprToString is the value of expression, a constant string, and whether it is one. It is staticcheck's
// code.ExprToString, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
func ExprToString(context *rule.Context, expression ast.Expr) (string, bool) {
	value := context.TypesInfo.Types[expression].Value
	if value == nil {
		return "", false
	}
	if value.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(value), true
}
