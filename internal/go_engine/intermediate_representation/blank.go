package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// A blank is a dummy variable whose name is "_".
// It is not reified: loads are illegal and stores are ignored.
type blank struct{}

func (blank blank) load(function *Function, source ast.Node) Value {
	// Invariant: go/types rejects _ as a value, so no op-assignment or increment loads from it.
	panic("blank.load is illegal")
}

func (blank blank) store(function *Function, value Value, source ast.Node) {
	store := &BlankStore{
		Value: value,
	}
	function.emit(store, source)
}

func (blank blank) address(function *Function) Value {
	// Invariant: go/types rejects &_ and any other use of _ that needs its address.
	panic("blank var is not addressable")
}

func (blank blank) typ() types.Type {
	// This should be the type of the blank Ident; the typechecker
	// doesn't provide this yet, but fortunately, we don't need it
	// yet either.
	// Invariant: assign checks for a blank before it asks a location's type, and go/types rejects _ in an
	// op-assignment or increment, the other places that ask it.
	panic("blank.typ is unimplemented")
}
