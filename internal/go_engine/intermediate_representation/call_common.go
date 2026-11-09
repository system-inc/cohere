package intermediate_representation

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"
)

// CallCommon is contained by Go, Defer and Call to hold the
// common parts of a function or method call.
//
// Each CallCommon exists in one of two modes, function call and
// interface method invocation, or "call" and "invoke" for short.
//
// 1. "call" mode: when Method is nil (!IsInvoke), a CallCommon
// represents an ordinary function call of the value in Value,
// which may be a *Builtin, a *Function or any other value of kind
// 'func'.
//
// Value may be one of:
//
//	(a) a *Function, indicating a statically dispatched call
//	    to a package-level function, an anonymous function, or
//	    a method of a named type.
//	(b) a *MakeClosure, indicating an immediately applied
//	    function literal with free variables.
//	(c) a *Builtin, indicating a statically dispatched call
//	    to a built-in function.
//	(d) any other value, indicating a dynamically dispatched
//	    function call.
//
// StaticCallee returns the identity of the callee in cases
// (a) and (b), nil otherwise.
//
// Arguments contains the arguments to the call.  If Value is a method,
// Arguments[0] contains the receiver parameter.
//
// Example printed form:
//
//	t2 = println(t0, t1)
//	go t3()
//	defer t5(...t6)
//
// 2. "invoke" mode: when Method is non-nil (IsInvoke), a CallCommon
// represents a dynamically dispatched call to an interface method.
// In this mode, Value is the interface value and Method is the
// interface's abstract method. The interface value may be a type
// parameter. Note: an interface method may be shared by multiple
// interfaces due to embedding; Value.Type() provides the specific
// interface used for this call.
//
// Value is implicitly supplied to the concrete method implementation
// as the receiver parameter; in other words, Arguments[0] holds not the
// receiver but the first true argument.
//
// Example printed form:
//
//	t1 = invoke t0.String()
//	go invoke t3.Run(t2)
//	defer invoke t4.Handle(...t5)
//
// For all calls to variadic functions (Signature().Variadic()),
// the last element of Arguments is a slice.
type CallCommon struct {
	Value     Value       // receiver (invoke mode) or func value (call mode)
	Method    *types.Func // interface method (invoke mode)
	Arguments []Value     // actual parameters (in static method call, includes receiver)

	source ast.Node
}

// IsInvoke returns true if this call has "invoke" (not "call") mode.
func (common *CallCommon) IsInvoke() bool {
	return common.Method != nil
}

func (common *CallCommon) Pos() token.Pos {
	if common.source != nil {
		return common.source.Pos()
	}
	return token.NoPos
}

// Signature returns the signature of the called function.
//
// For an "invoke"-mode call, the signature of the interface method is
// returned.
//
// In either "call" or "invoke" mode, if the callee is a method, its
// receiver is represented by sig.Recv, not sig.Params().At(0).
func (common *CallCommon) Signature() *types.Signature {
	if common.Method != nil {
		signature, isSignature := common.Method.Type().(*types.Signature)
		if !isSignature {
			// Invariant: go/types gives every *types.Func a *types.Signature as its type.
			panic(fmt.Sprintf("CallCommon.Signature: method %s has type %s, not a signature", common.Method,
				common.Method.Type()))
		}
		return signature
	}
	signature, isSignature := typeutil.CoreType(common.Value.Type()).(*types.Signature)
	if !isSignature {
		// Invariant: a call-mode value is the callee of a call go/types accepted, so its core type is a
		// function signature.
		panic(fmt.Sprintf("CallCommon.Signature: callee %s has type %s, whose core type is not a signature",
			common.Value, common.Value.Type()))
	}
	return signature
}

// StaticCallee returns the callee if this is a trivially static
// "call"-mode call to a function.
func (common *CallCommon) StaticCallee() *Function {
	switch callee := common.Value.(type) {
	case *Function:
		return callee
	case *MakeClosure:
		function, isFunction := callee.Function.(*Function)
		if !isFunction {
			// Invariant: the builder makes every closure from a *Function, as MakeClosure.Function says.
			panic(fmt.Sprintf("StaticCallee: closure %s is over %T, not a *Function", callee.Name(), callee.Function))
		}
		return function
	}
	return nil
}

// Description returns a description of the mode of this call suitable
// for a user interface, e.g., "static method call".
func (common *CallCommon) Description() string {
	switch callee := common.Value.(type) {
	case *Builtin:
		return "built-in function call"
	case *MakeClosure:
		return "static function closure call"
	case *Function:
		if callee.Signature.Recv() != nil {
			return "static method call"
		}
		return "static function call"
	}
	if common.IsInvoke() {
		return "dynamic method call" // ("invoke" mode)
	}
	return "dynamic function call"
}

func (common *CallCommon) Operands(rands []*Value) []*Value {
	operands := append(rands, &common.Value)
	for index := range common.Arguments {
		operands = append(operands, &common.Arguments[index])
	}
	return operands
}
