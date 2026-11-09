package staticcheck_code

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// PackageFunction is the function of the package at path that callee names, and false for anything else, so a port
// whose query names a package's function, `(Symbol "fmt.Sprintf")`, can ask which of the package's functions a callee
// is once and compare its name after. It resolves a callee as honnef.co/go/tools@v0.8.1 pattern/match.go's Symbol
// does: without parentheses, without one instantiation's type arguments, and then an identifier, a dot import's among
// them, or a selector's selected name, through the identifiers' uses, since a callee is never a definition. Symbol
// also names variables, constants and types, so this answers only for a query whose names are all functions, where a
// function is the only answer that could match. A method's full name is its receiver's and its own, never its
// package's path and its own, so a method is none.
func PackageFunction(context *rule.Context, callee ast.Expr, path string) (*types.Func, bool) {
	named := ast.Unparen(callee)
	switch instantiated := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := named.(type) {
	case *ast.Ident:
		object = context.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		object = context.TypesInfo.Uses[named.Sel]
	default:
		return nil, false
	}
	function, isFunction := object.(*types.Func)
	if !isFunction || function.Pkg() == nil || function.Pkg().Path() != path {
		return nil, false
	}
	if function.Signature().Recv() != nil {
		return nil, false
	}
	return function, true
}
