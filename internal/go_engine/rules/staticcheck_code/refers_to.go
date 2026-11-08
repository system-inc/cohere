package staticcheck_code

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// RefersTo is whether expression holds an identifier that defines or uses object, at any depth. It is staticcheck's
// code.RefersTo, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// A nil object is referred to by an identifier the type checker recorded no object for, such as the blank identifier
// on an assignment's left, as staticcheck's comparison of the two finds it.
func RefersTo(context *rule.Context, expression ast.Expr, object types.Object) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier {
			return true
		}
		if context.TypesInfo.ObjectOf(identifier) == object {
			found = true
			return false
		}
		return true
	})
	return found
}
