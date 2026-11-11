package copy_lock

import (
	"go/ast"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/type_predicates/locks"
)

// lockPathOfValue is the path to a lock in the value value denotes, nil when it holds none or makes a new value
// rather than copying one: a composite literal, or a call, which may return a zero value, or a pointer to one
// through `*`. Parentheses are looked through. x/tools' lockPathRhs.
func lockPathOfValue(context *rule.Context, value ast.Expr) locks.Path {
	unparenthesized := ast.Unparen(value)
	switch made := unparenthesized.(type) {
	case *ast.CompositeLit, *ast.CallExpr:
		return nil
	case *ast.StarExpr:
		if _, isCall := ast.Unparen(made.X).(*ast.CallExpr); isCall {
			return nil
		}
	}
	// A type or a package name is no value, and Types holds nothing for an expression the checker could not type.
	if typeAndValue, isTyped := context.TypesInfo.Types[unparenthesized]; isTyped && typeAndValue.IsValue() {
		return locks.Of(typeAndValue.Type)
	}
	return nil
}
