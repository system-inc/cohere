package intermediate_representation

import (
	"go/ast"
	"go/types"
)

type compositeElement struct {
	cv          *CompositeValue
	index       int
	elementType types.Type

	// expression is the element's syntax, for the debug references a package built with SetDebugMode records;
	// nil where there is none.
	expression ast.Expr
}

func (element *compositeElement) load(function *Function, source ast.Node) Value {
	// Invariant: compLit makes a compositeElement only as the target of a store, through assign or the store
	// buffer, neither of which loads from it.
	panic("not implemented")
}

func (element *compositeElement) store(function *Function, value Value, source ast.Node) {
	converted := emitConv(function, value, element.elementType, source)
	element.cv.Values[element.index] = converted
	if element.expression != nil {
		// converted is value, converted for assignability.
		emitDebugReference(function, element.expression, converted, false)
	}
}

func (element *compositeElement) address(function *Function) Value {
	// Invariant: compLit makes a compositeElement only as the target of a store, through assign or the store
	// buffer, and assign takes the address only of an *address location.
	panic("not implemented")
}

func (element *compositeElement) storedType() types.Type {
	return element.elementType
}
