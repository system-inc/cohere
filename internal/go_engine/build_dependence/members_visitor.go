package build_dependence

import (
	"go/ast"

	"github.com/system-inc/go-types/types"
)

// membersVisitor is collectMembers' walk of a unit's header: the unit, the owners of the members it declares, and
// which of their bits takes each. Declaring a member walks no header, so one walk at a time is all a collector holds.
type membersVisitor struct {
	collecting      *collector
	unit            *unitRecord
	owners          []types.Object
	ownerUnderlying bool
}

// Visit declares the fields of a struct type and the methods of an interface type.
func (visitor *membersVisitor) Visit(node ast.Node) ast.Visitor {
	switch node := node.(type) {
	case *ast.StructType:
		visitor.collecting.collectFields(visitor.unit, visitor.owners, visitor.ownerUnderlying, node)
	case *ast.InterfaceType:
		visitor.collecting.collectInterfaceMethods(visitor.unit, visitor.owners, visitor.ownerUnderlying, node)
	}
	return visitor
}
