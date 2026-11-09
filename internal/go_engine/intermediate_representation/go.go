package intermediate_representation

import "go/ast"

// The Go instruction creates a new goroutine and calls the specified
// function within it.
//
// See CallCommon for generic function call documentation.
//
// Pos() returns the ast.GoStmt.Go.
//
// Example printed form:
//
//	go println(t0, t1)
//	go t3()
//	go invoke t5.Println(...t6)
type Go struct {
	anInstruction
	Call CallCommon
}

func (goInstruction *Go) Common() *CallCommon { return &goInstruction.Call }

func (goInstruction *Go) setSource(source ast.Node) {
	goInstruction.source = source
	goInstruction.Call.source = source
}

func (goInstruction *Go) Value() *Call { return nil }

func (goInstruction *Go) Operands(rands []*Value) []*Value {
	return goInstruction.Call.Operands(rands)
}
