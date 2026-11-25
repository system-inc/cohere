package intermediate_representation

import "go/ast"

// The Defer instruction pushes the specified call onto a stack of
// functions to be called by a RunDefers instruction or by a panic.
//
// If DeferStack != nil, it indicates the defer list that the defer is
// added to. Defer list values come from the Builtin function
// ssa:deferstack. Calls to ssa:deferstack() produces the defer stack
// of the current function frame. DeferStack allows for deferring into an
// alternative function stack than the current function.
//
// See CallCommon for generic function call documentation.
//
// Pos() returns the ast.DeferStmt.Defer.
//
// Example printed form:
//
//	defer println(t0, t1)
//	defer t3()
//	defer invoke t5.Println(...t6)
type Defer struct {
	anInstruction
	Call       CallCommon
	DeferStack Value // stack of deferred functions (from ssa:deferstack() intrinsic) onto which this function is pushed
}

func (deferInstruction *Defer) Common() *CallCommon { return &deferInstruction.Call }

func (deferInstruction *Defer) setSource(source ast.Node) {
	deferInstruction.source = source
	deferInstruction.Call.source = source
}

func (deferInstruction *Defer) Value() *Call { return nil }

func (deferInstruction *Defer) Operands(rands []*Value) []*Value {
	return append(deferInstruction.Call.Operands(rands), &deferInstruction.DeferStack)
}
