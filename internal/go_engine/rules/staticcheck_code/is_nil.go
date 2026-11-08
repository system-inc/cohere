package staticcheck_code

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsNil is whether expression is the predeclared nil, as the type checker recorded it, under parentheses or not. It
// is staticcheck's code.IsNil, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
func IsNil(context *rule.Context, expression ast.Expr) bool {
	return context.TypesInfo.Types[expression].IsNil()
}
