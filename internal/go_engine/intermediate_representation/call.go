package intermediate_representation

import "go/ast"

// The Call instruction represents a function or method call.
//
// The Call instruction yields the function result if there is exactly
// one.  Otherwise it returns a tuple, the components of which are
// accessed via Extract.
//
// See CallCommon for generic function call documentation.
//
// Pos() returns the ast.CallExpr.Lparen, if explicit in the source.
//
// Example printed form:
//
//	t2 = println(t0, t1)
//	t4 = t3()
//	t7 = invoke t5.Println(...t6)
type Call struct {
	register
	Call CallCommon
}

func (call *Call) Common() *CallCommon { return &call.Call }

func (call *Call) setSource(source ast.Node) { call.source = source; call.Call.source = source }

func (call *Call) Value() *Call { return call }

func (call *Call) Operands(rands []*Value) []*Value {
	return call.Call.Operands(rands)
}
