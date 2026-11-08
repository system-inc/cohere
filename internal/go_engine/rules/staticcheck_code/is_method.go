package staticcheck_code

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsMethod is whether expression selects a method value whose signature is identical to method's, and, when name is
// not empty, whose selected name is name: staticcheck's code.IsMethod. A method expression, a field or a qualified
// identifier is no method value, and a signature is compared without its receiver, so any type's method of the same
// parameters and results answers true.
func IsMethod(context *rule.Context, expression *ast.SelectorExpr, name string, method *types.Signature) bool {
	if name != "" && expression.Sel.Name != name {
		return false
	}
	selection, isSelection := context.TypesInfo.Selections[expression]
	if !isSelection || selection.Kind() != types.MethodVal {
		return false
	}
	return types.Identical(selection.Type(), method)
}
