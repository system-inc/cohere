package build_dependence

import (
	"go/ast"
	"go/types"
)

// usesVisitor is addUses' walk of one root: the object whose bit depends on what the root uses, which bit, and whether
// a type name or var is read in full. Adding a use walks no syntax, so one walk at a time is all a collector holds.
type usesVisitor struct {
	collecting     *collector
	from           types.Object
	fromUnderlying bool
	full           bool
}

// Visit adds the use of the object an identifier or selector resolves to.
func (visitor *usesVisitor) Visit(node ast.Node) ast.Visitor {
	var used types.Object
	switch node := node.(type) {
	case *ast.Ident:
		used = visitor.collecting.typesInformation.Uses[node]
	case *ast.SelectorExpr:
		if selection := visitor.collecting.typesInformation.Selections[node]; selection != nil {
			used = selection.Obj()
		}
	}
	if used != nil {
		visitor.collecting.addUse(visitor.from, visitor.fromUnderlying, visitor.full, used)
	}
	return visitor
}
