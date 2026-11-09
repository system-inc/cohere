package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// A lazyAddress is an lvalue whose address is the result of an instruction.
// These work like an *address except a new address.address() Value
// is created on each load, store and address call.
// A lazyAddress can be used to control when a side effect (nil pointer
// dereference, index out of bounds) of using a location happens.
type lazyAddress struct {
	addr         func(function *Function) Value // emit to fn the computation of the address
	locationType types.Type                     // type of the location

	// expression is the syntax of the value (not the address), for the debug references a package built with
	// SetDebugMode records; nil where there is none.
	expression ast.Expr
}

func (address *lazyAddress) load(function *Function, source ast.Node) Value {
	load := emitLoad(function, address.addr(function), source)
	return load
}

func (address *lazyAddress) store(function *Function, value Value, source ast.Node) {
	store := emitStore(function, address.addr(function), value, source)
	if address.expression != nil {
		// store.Value is value, converted for assignability.
		emitDebugReference(function, address.expression, store.Value, false)
	}
}

func (address *lazyAddress) address(function *Function) Value {
	computed := address.addr(function)
	if address.expression != nil {
		emitDebugReference(function, address.expression, computed, true)
	}
	return computed
}

func (address *lazyAddress) typ() types.Type { return address.locationType }
