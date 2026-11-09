package intermediate_representation

import (
	"go/ast"
	"go/types"
)

// A blank is a dummy variable whose name is "_".
// It is not reified: loads are illegal and stores are ignored.
type blank struct{}

func (blank blank) load(function *Function, source ast.Node) Value {
	panic("blank.load is illegal")
}

func (blank blank) store(function *Function, value Value, source ast.Node) {
	store := &BlankStore{
		Value: value,
	}
	function.emit(store, source)
}

func (blank blank) address(function *Function) Value {
	panic("blank var is not addressable")
}

func (blank blank) typ() types.Type {
	// This should be the type of the blank Ident; the typechecker
	// doesn't provide this yet, but fortunately, we don't need it
	// yet either.
	panic("blank.typ is unimplemented")
}
