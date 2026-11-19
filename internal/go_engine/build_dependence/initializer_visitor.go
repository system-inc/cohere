package build_dependence

import (
	"go/ast"
)

// initializerVisitor is collectValue's walk of the initializers of a var that declares no type: it gathers into its
// collector's passedTypes every type an expression under them has and the type of every object an identifier or
// selector there uses, each once. The var's type is taken from them by an index, a dereference, a receive, a call or
// an inference, any of which can reach into a named type's underlying, so the var's bit reads each in full.
type initializerVisitor struct {
	collecting *collector
}

// Visit gathers an expression's type and the type of the object it uses, and leaves a function literal's body alone:
// the literal's type is its signature, gathered here, and nothing in its body moves the type of the var.
func (visitor *initializerVisitor) Visit(node ast.Node) ast.Visitor {
	typesInformation := visitor.collecting.typesInformation
	if expression, isExpression := node.(ast.Expr); isExpression {
		if typeAndValue, isRecorded := typesInformation.Types[expression]; isRecorded {
			visitor.collecting.passType(typeAndValue.Type)
		}
	}
	switch node := node.(type) {
	case *ast.Ident:
		// A generic function's own type, its constraints included, beside the instance the expression's type is.
		if used := typesInformation.Uses[node]; used != nil {
			visitor.collecting.passType(used.Type())
		}
	case *ast.SelectorExpr:
		if selection := typesInformation.Selections[node]; selection != nil {
			visitor.collecting.passType(selection.Obj().Type())
		}
	case *ast.FuncLit:
		return nil
	}
	return visitor
}
