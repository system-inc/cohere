package build_dependence

import (
	"go/ast"

	"github.com/system-inc/go-types/types"
)

// usesVisitor is addUses' walk of one root: the object whose bit depends on what the root uses, which bit, and whether
// a type name or var is read in full. Adding a use walks no syntax, so one walk at a time is all a collector holds.
type usesVisitor struct {
	collecting     *collector
	from           types.Object
	fromUnderlying bool
	full           bool
}

// Visit adds the use of the object an identifier or selector resolves to. An array's length is a value, read in full
// as a constant's is, though the type around it is read by name: `[64 - unsafe.Sizeof(m{})]byte` moves with m's
// layout, as runtime's mPadded does by platform.
func (visitor *usesVisitor) Visit(node ast.Node) ast.Visitor {
	var used types.Object
	switch node := node.(type) {
	case *ast.ArrayType:
		if visitor.full || node.Len == nil {
			return visitor
		}
		// The walk's own visitor reads the length, full for it alone, so no other is made: an array type within the
		// length returns at the check above, so full is still set until the length's walk is done.
		visitor.full = true
		ast.Walk(visitor, node.Len)
		visitor.full = false
		ast.Walk(visitor, node.Elt)
		return nil
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
