package intermediate_representation

// The MakeClosure instruction yields a closure value whose code is
// Function and whose free variables' values are supplied by Bindings.
//
// Type() returns a (possibly named) *types.Signature.
//
// Pos() returns the ast.FuncLit.Type.Func for a function literal
// closure or the ast.SelectorExpr.Sel for a bound method closure.
//
// Example printed form:
//
//	t0 = make closure anon@1.2 [x y z]
//	t1 = make closure bound$(main.I).add [i]
type MakeClosure struct {
	register
	Function Value   // always a *Function
	Bindings []Value // values for each free variable in Function.FreeVariables
}

func (closure *MakeClosure) Operands(rands []*Value) []*Value {
	operands := append(rands, &closure.Function)
	for index := range closure.Bindings {
		operands = append(operands, &closure.Bindings[index])
	}
	return operands
}
