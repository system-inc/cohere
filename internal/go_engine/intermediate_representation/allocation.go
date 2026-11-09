package intermediate_representation

import "go/types"

// The Allocation instruction reserves space for a variable of the given type,
// zero-initializes it, and yields its address.
//
// Allocation values are always addresses, and have pointer types, so the
// type of the allocated variable is actually
// Type().Underlying().(*types.Pointer).Elem().
//
// If Heap is false, Allocation zero-initializes the same local variable in
// the call frame and returns its address; in this case the Allocation must
// be present in Function.Locals. We call this a "local" alloc.
//
// If Heap is true, Allocation allocates a new zero-initialized variable
// each time the instruction is executed. We call this a "new" alloc.
//
// When Allocation is applied to a channel, map or slice type, it returns
// the address of an uninitialized (nil) reference of that kind; store
// the result of MakeSlice, MakeMap or MakeChannel in that location to
// instantiate these types.
//
// Pos() returns the ast.CompositeLit.Lbrace for a composite literal,
// or the ast.CallExpr.Rparen for a call to new() or for a call that
// allocates a varargs slice.
//
// Example printed form:
//
//	t0 = local int
//	t1 = new int
type Allocation struct {
	register
	Heap bool
}

func (allocation *Allocation) Type() types.Type { return allocation.typ }

func (allocation *Allocation) Referrers() *[]Instruction { return &allocation.referrers }

func (allocation *Allocation) Operands(rands []*Value) []*Value {
	return rands
}
