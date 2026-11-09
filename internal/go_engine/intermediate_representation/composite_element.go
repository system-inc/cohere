package intermediate_representation

import (
	"go/ast"
	"go/types"
)

type compositeElement struct {
	cv          *CompositeValue
	index       int
	elementType types.Type
}

func (element *compositeElement) load(function *Function, source ast.Node) Value {
	panic("not implemented")
}

func (element *compositeElement) store(function *Function, value Value, source ast.Node) {
	value = emitConv(function, value, element.elementType, source)
	element.cv.Values[element.index] = value
}

func (element *compositeElement) address(function *Function) Value {
	panic("not implemented")
}

func (element *compositeElement) typ() types.Type {
	return element.elementType
}
