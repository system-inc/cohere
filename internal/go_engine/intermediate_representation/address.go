package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// An address is an lvalue represented by a true pointer.
type address struct {
	addr Value // must have a pointer core type.

	// expression is the syntax of the value (not the address), for the debug references a package built with
	// SetDebugMode records; nil where there is none.
	expression ast.Expr
}

func (address *address) load(function *Function, source ast.Node) Value {
	return emitLoad(function, address.addr, source)
}

func (address *address) store(function *Function, value Value, source ast.Node) {
	store := emitStore(function, address.addr, value, source)
	if address.expression != nil {
		// store.Value is value, converted for assignability.
		emitDebugReference(function, address.expression, store.Value, false)
	}
}

func (address *address) address(function *Function) Value {
	if address.expression != nil {
		emitDebugReference(function, address.expression, address.addr, true)
	}
	return address.addr
}

func (address *address) typ() types.Type {
	return deref(address.addr.Type())
}
