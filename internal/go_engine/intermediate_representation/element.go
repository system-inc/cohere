package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// An element is an lvalue represented by m[k], the location of an
// element of a map.  These locations are not addressable
// since pointers cannot be formed from them, but they do support
// load() and store().
type element struct {
	mapValue, key Value      // map
	elementType   types.Type // map element type
}

func (element *element) load(function *Function, source ast.Node) Value {
	lookup := &MapLookup{
		X:     element.mapValue,
		Index: element.key,
	}
	lookup.setType(element.elementType)
	return function.emit(lookup, source)
}

func (element *element) store(function *Function, value Value, source ast.Node) {
	up := &MapUpdate{
		Map:   element.mapValue,
		Key:   element.key,
		Value: emitConv(function, value, element.elementType, source),
	}
	function.emit(up, source)
}

func (element *element) address(function *Function) Value {
	// Invariant: go/types rejects every use of a map element that needs its address: &m[k], assigning to a field
	// or element of one, slicing one, or calling a pointer method on one.
	panic("map elements are not addressable")
}

func (element *element) typ() types.Type {
	return element.elementType
}
