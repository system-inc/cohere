package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// An address is an lvalue represented by a true pointer.
type address struct {
	addr Value // must have a pointer core type.
}

func (address *address) load(function *Function, source ast.Node) Value {
	return emitLoad(function, address.addr, source)
}

func (address *address) store(function *Function, value Value, source ast.Node) {
	emitStore(function, address.addr, value, source)
}

func (address *address) address(function *Function) Value {
	return address.addr
}

func (address *address) typ() types.Type {
	return deref(address.addr.Type())
}
