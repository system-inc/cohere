package canonical_function_hash

import (
	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// functionName is a function as a reader finds it: `Class.method` for a class member, a function's own name, or the
// variable, property or field a function value is assigned to, and `<anonymous>` when none names it.
func functionName(node *ast.Node) string {
	own := nameText(node.Name())
	parent := node.Parent
	switch node.Kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
		if node.Kind == ast.KindConstructor {
			own = "constructor"
		}
		if parent != nil && ast.IsClassLike(parent) {
			className := nameText(parent.Name())
			if className == "" {
				className = "<class>"
			}
			return className + "." + own
		}
		return own
	}
	if own != "" {
		return own
	}
	for parent != nil {
		switch parent.Kind {
		case ast.KindVariableDeclaration, ast.KindPropertyAssignment, ast.KindPropertyDeclaration:
			return nameText(parent.Name())
		case ast.KindParenthesizedExpression, ast.KindAsExpression, ast.KindSatisfiesExpression:
			parent = parent.Parent
			continue
		}
		break
	}
	return "<anonymous>"
}

// nameText is a declaration name's text, `[computed]` for a computed one, empty for none.
func nameText(name *ast.Node) string {
	if name == nil {
		return ""
	}
	if name.Kind == ast.KindComputedPropertyName {
		return "[computed]"
	}
	return name.Text()
}
