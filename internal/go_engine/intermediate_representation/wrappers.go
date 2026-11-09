// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines synthesis of Functions that delegate to declared
// methods; they come in three kinds:
//
// (1) wrappers: methods that wrap declared methods, performing
//     implicit pointer indirections and embedded field selections.
//
// (2) thunks: funcs that wrap declared methods.  Like wrappers,
//     thunks perform indirections and field selections. The thunk's
//     first parameter is used as the receiver for the method call.
//
// (3) bounds: funcs that wrap declared methods.  The bound's sole
//     free variable, supplied by a closure, is used as the receiver
//     for the method call.  No indirections or field selections are
//     performed since they can be done before the call.

import (
	"fmt"
	"go/types"
)

// -- wrappers -----------------------------------------------------------

// createWrapper returns a synthetic method that delegates to the
// declared method denoted by meth.Obj(), first performing any
// necessary pointer indirections or field selections implied by meth.
//
// The resulting method's receiver type is meth.Recv().
//
// This function is versatile but quite subtle!  Consider the
// following axes of variation when making changes:
//   - optional receiver indirection
//   - optional implicit field selections
//   - optional method type arguments
//   - meth.Obj() may denote a concrete or an interface method
//   - the result may be a thunk or a wrapper.
func createWrapper(prog *Program, sel *selection, targs []types.Type) *Function {
	object := sel.object.(*types.Func) // the declared function
	name, sig := maybeInstance(prog, object.Name(), sel.typ.(*types.Signature), targs)

	var description string
	if sel.kind == types.MethodExpr {
		name += "$thunk"
		description = "thunk"
	} else {
		description = "wrapper"
	}

	description = fmt.Sprintf("%s for %s", description, object)
	/* method wrapper */
	return &Function{
		name:      name,
		method:    sel,
		object:    object,
		Signature: sig,
		Synthetic: description,
		Program:   prog,
		position:  object.Pos(),
		typeargs:  targs,
		// wrappers have no syntax
		build:     (*builder).buildWrapper,
		info:      nil,
		goversion: "",
	}
}

// maybeInstance returns name and sig instantiated to reflect any type arguments in targs.
func maybeInstance(prog *Program, name string, sig *types.Signature, targs []types.Type) (string, *types.Signature) {
	if len(targs) > 0 {
		name = fmt.Sprintf("%s%s", name, targstr(targs))
		instSig, instantiateError := types.Instantiate(prog.ctxt, sig, targs, false)
		if instantiateError != nil {
			// validate was false, we should never get an error
			panic(instantiateError)
		}
		sig = prog.canon.Type(instSig).(*types.Signature)
	}
	return name, sig
}

// buildWrapper builds fn.Body for a method wrapper.
func (packageBuilder *builder) buildWrapper(function *Function) {
	var recv *types.Var // wrapper's receiver or thunk's params[0]
	var start int       // first regular param
	if function.method.kind == types.MethodExpr {
		recv = function.Signature.Params().At(0)
		start = 1
	} else {
		recv = function.Signature.Recv()
	}

	function.startBody()
	function.addSpilledParameter(recv, nil)
	createParameters(function, start)

	indices := function.method.index

	var local Value = function.Locals[0] // spilled receiver
	if isPointer(function.method.recv) {
		local = emitLoad(function, local, nil)

		// For simple indirection wrappers, perform an informative nil-check:
		// "value method (T).f called using nil *T pointer"
		if len(indices) == 1 && !isPointer(recvType(function.object)) {
			var call Call
			call.Call.Value = &Builtin{
				name: "ssa:wrapnilchk",
				sig: types.NewSignatureType(
					nil,
					nil,
					nil,
					types.NewTuple(anonVariable(function.method.recv), anonVariable(tString), anonVariable(tString)),
					types.NewTuple(anonVariable(function.method.recv)),
					false,
				),
			}
			call.Call.Arguments = []Value{
				local,
				stringConstant(deref(function.method.recv).String(), nil),
				stringConstant(function.method.object.Name(), nil),
			}
			call.setType(local.Type())
			local = function.emit(&call, nil)
		}
	}

	// Invariant: v is a pointer, either
	//   value of *A receiver param, or
	// address of  A spilled receiver.

	// We use pointer arithmetic (FieldAddress possibly followed by
	// Load) in preference to value extraction (Field possibly
	// preceded by Load).

	local = emitImplicitSelections(function, local, indices[:len(indices)-1], nil)

	// Invariant: v is a pointer, either
	//   value of implicit *C field, or
	// address of implicit  C field.

	var call Call
	if resolvedType := recvType(function.object); !types.IsInterface(resolvedType) { // concrete method
		if !isPointer(resolvedType) {
			local = emitLoad(function, local, nil)
		}
		call.Call.Value = function.Program.objectMethod(function.object, function.typeargs, packageBuilder)
		call.Call.Arguments = append(call.Call.Arguments, local)
	} else {
		call.Call.Method = function.object
		call.Call.Value = emitLoad(function, local, nil) // interface (possibly a typeparam)
	}
	for _, argument := range function.Parameters[1:] {
		call.Call.Arguments = append(call.Call.Arguments, argument)
	}
	emitTailCall(function, &call, nil)
	function.finishBody()
}

// createParameters creates parameters for wrapper method fn based on its
// Signature.Params, which do not include the receiver.
// start is the index of the first regular parameter to use.
func createParameters(function *Function, start int) {
	tparams := function.Signature.Params()
	for index, length := start, tparams.Len(); index < length; index++ {
		function.addParamVariable(tparams.At(index), nil)
	}
}

// -- bounds -----------------------------------------------------------

// createBound returns a bound method wrapper (or "bound"), a synthetic
// function that delegates to a concrete or interface method denoted
// by obj.  The resulting function has no receiver, but has one free
// variable which will be used as the method's receiver in the
// tail-call.
//
// Use MakeClosure with such a wrapper to construct a bound method
// closure.  e.g.:
//
//	type T int          or:  type T interface { meth() }
//	func (t T) meth()
//	var t T
//	f := t.meth
//	f() // calls t.meth()
//
// f is a closure of a synthetic wrapper defined as if by:
//
//	f := func() { return t.meth() }
//
// Unlike createWrapper, createBound need perform no indirection or field
// selections because that can be done before the closure is
// constructed.
func createBound(prog *Program, object *types.Func, targs []types.Type) *Function {
	description := fmt.Sprintf("bound method wrapper for %s", object)
	name, sig := maybeInstance(prog, object.Name(), object.Type().(*types.Signature), targs)

	/* bound method wrapper */
	function := &Function{
		name:      name + "$bound",
		object:    object,
		Signature: changeRecv(sig, nil), // drop receiver
		Synthetic: description,
		Program:   prog,
		position:  object.Pos(),
		typeargs:  targs,
		// wrappers have no syntax
		build:     (*builder).buildBound,
		info:      nil,
		goversion: "",
	}
	function.FreeVariables = []*FreeVariable{{name: "recv", typ: recvType(object), parent: function}} // (cyclic)
	return function
}

// buildBound builds fn.Body for a bound method closure.
func (packageBuilder *builder) buildBound(function *Function) {
	function.startBody()
	createParameters(function, 0)
	var call Call

	recv := function.FreeVariables[0]
	if !types.IsInterface(recvType(function.object)) { // concrete
		call.Call.Value = function.Program.objectMethod(function.object, function.typeargs, packageBuilder)
		call.Call.Arguments = []Value{recv}
	} else {
		call.Call.Method = function.object
		call.Call.Value = recv // interface (possibly a typeparam)
	}
	for _, argument := range function.Parameters {
		call.Call.Arguments = append(call.Call.Arguments, argument)
	}
	emitTailCall(function, &call, nil)
	function.finishBody()
}

// -- thunks -----------------------------------------------------------

// createThunk returns a thunk, a synthetic function that delegates to a
// concrete or interface method denoted by sel.obj.  The resulting
// function has no receiver, but has an additional (first) regular
// parameter.
//
// Precondition: sel.kind == types.MethodExpr.
//
//	type T int          or:  type T interface { meth() }
//	func (t T) meth()
//	f := T.meth
//	var t T
//	f(t) // calls t.meth()
//
// f is a synthetic wrapper defined as if by:
//
//	f := func(t T) { return t.meth() }
func createThunk(prog *Program, sel *selection, targs []types.Type) *Function {
	if sel.kind != types.MethodExpr {
		panic(sel)
	}

	function := createWrapper(prog, sel, targs)
	if function.Signature.Recv() != nil {
		panic(function) // unexpected receiver
	}
	return function
}

func changeRecv(signature *types.Signature, recv *types.Var) *types.Signature {
	return types.NewSignatureType(recv, nil, nil, signature.Params(), signature.Results(), signature.Variadic())
}

// A local version of *types.Selection.
// Needed for some additional control, such as creating a MethodExpr for an instantiation.
type selection struct {
	kind     types.SelectionKind
	recv     types.Type
	typ      types.Type
	object   types.Object
	index    []int
	indirect bool
}

func toSelection(sel *types.Selection) *selection {
	return &selection{
		kind:     sel.Kind(),
		recv:     sel.Recv(),
		typ:      sel.Type(),
		object:   sel.Obj(),
		index:    sel.Index(),
		indirect: sel.Indirect(),
	}
}

// -- instantiations --------------------------------------------------

// buildInstantiationWrapper builds the body of an instantiation
// wrapper fn. The body calls the original generic function,
// bracketed by ChangeType conversions on its arguments and results.
func (packageBuilder *builder) buildInstantiationWrapper(function *Function) {
	orig := function.topLevelOrigin
	sig := function.Signature

	function.startBody()
	if sig.Recv() != nil {
		function.addParamVariable(sig.Recv(), nil)
	}
	createParameters(function, 0)

	// Create body. Add a call to origin generic function
	// and make type changes between argument and parameters,
	// as well as return values.
	var call Call
	call.Call.Value = orig
	if resultTypes := orig.Signature.Results(); resultTypes.Len() == 1 {
		call.typ = resultTypes.At(0).Type()
	} else {
		call.typ = resultTypes
	}

	// parameter of instance becomes an argument to the call
	// to the original generic function.
	argumentOffset := 0
	for index, argument := range function.Parameters {
		var typ types.Type
		if index == 0 && sig.Recv() != nil {
			typ = orig.Signature.Recv().Type()
			argumentOffset = 1
		} else {
			typ = orig.Signature.Params().At(index - argumentOffset).Type()
		}
		call.Call.Arguments = append(call.Call.Arguments, emitTypeCoercion(function, argument, typ, nil))
	}

	results := function.emit(&call, nil)
	var ret Return
	switch resultTypes := sig.Results(); resultTypes.Len() {
	case 0:
		// no results, do nothing.
	case 1:
		ret.Results = []Value{emitTypeCoercion(function, results, resultTypes.At(0).Type(), nil)}
	default:
		for index := 0; index < sig.Results().Len(); index++ {
			value := emitExtract(function, results, index, nil)
			ret.Results = append(ret.Results, emitTypeCoercion(function, value, resultTypes.At(index).Type(), nil))
		}
	}

	function.emit(&ret, nil)
	function.currentBlock = nil

	function.finishBody()
}
