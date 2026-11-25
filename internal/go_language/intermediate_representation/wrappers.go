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
func createWrapper(program *Program, selection *selection, targs []types.Type) *Function {
	object, isFunction := selection.object.(*types.Func) // the declared function
	if !isFunction {
		// Invariant: createThunk is the only caller and passes only a method expression, which selects a method.
		panic(fmt.Sprintf("createWrapper: selection of %s is not a method", selection.object))
	}
	selectionSignature, isSignature := selection.selectionType.(*types.Signature)
	if !isSignature {
		// Invariant: the type of a method expression is a function signature, the receiver its first parameter.
		panic(fmt.Sprintf(
			"createWrapper: method expression %s has type %s, not a signature",
			object,
			selection.selectionType,
		))
	}
	name, sig := maybeInstance(program, object.Name(), selectionSignature, targs)

	var description string
	if selection.kind == types.MethodExpr {
		name += "$thunk"
		description = "thunk"
	} else {
		description = "wrapper"
	}

	description = fmt.Sprintf("%s for %s", description, object)
	/* method wrapper */
	return &Function{
		name:      name,
		method:    selection,
		object:    object,
		Signature: sig,
		Synthetic: description,
		Program:   program,
		position:  object.Pos(),
		typeargs:  targs,
		// wrappers have no syntax
		build:     (*builder).buildWrapper,
		info:      nil,
		goversion: "",
	}
}

// maybeInstance returns name and sig instantiated to reflect any type arguments in targs.
func maybeInstance(program *Program, name string, sig *types.Signature, targs []types.Type) (string, *types.Signature) {
	instanceName, instanceSignature := name, sig
	if len(targs) > 0 {
		instanceName = fmt.Sprintf("%s%s", name, targstr(targs))
		instSig, instantiateError := types.Instantiate(program.ctxt, sig, targs, false)
		if instantiateError != nil {
			// Invariant: with validate false, go/types documents that Instantiate never returns an error.
			panic(instantiateError)
		}
		canonicalSignature, isSignature := program.canon.Type(instSig).(*types.Signature)
		if !isSignature {
			// Invariant: an instance of a signature is a signature, and the canonizer returns a type identical to
			// the one it is given.
			panic(fmt.Sprintf("maybeInstance: instance %s of %s is not a signature", instSig, sig))
		}
		instanceSignature = canonicalSignature
	}
	return instanceName, instanceSignature
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
func createBound(program *Program, object *types.Func, targs []types.Type) *Function {
	description := fmt.Sprintf("bound method wrapper for %s", object)
	methodSignature, isSignature := object.Type().(*types.Signature)
	if !isSignature {
		// Invariant: go/types gives every *types.Func a *types.Signature as its type.
		panic(fmt.Sprintf("createBound: method %s has type %s, not a signature", object, object.Type()))
	}
	name, sig := maybeInstance(program, object.Name(), methodSignature, targs)

	/* bound method wrapper */
	function := &Function{
		name:      name + "$bound",
		object:    object,
		Signature: changeReceiver(sig, nil), // drop receiver
		Synthetic: description,
		Program:   program,
		position:  object.Pos(),
		typeargs:  targs,
		// wrappers have no syntax
		build:     (*builder).buildBound,
		info:      nil,
		goversion: "",
	}
	function.FreeVariables = []*FreeVariable{
		{name: "recv", valueType: methodReceiverType(object), parent: function},
	} // (cyclic)
	return function
}

// -- thunks -----------------------------------------------------------

// createThunk returns a thunk, a synthetic function that delegates to a
// concrete or interface method denoted by selection.object.  The resulting
// function has no receiver, but has an additional (first) regular
// parameter.
//
// Precondition: selection.kind == types.MethodExpr.
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
func createThunk(program *Program, selection *selection, targs []types.Type) *Function {
	if selection.kind != types.MethodExpr {
		// Invariant: the builder calls createThunk only in its case for a method expression selection.
		panic(selection)
	}

	function := createWrapper(program, selection, targs)
	if function.Signature.Recv() != nil {
		// Invariant: a method expression's signature takes its receiver as the first parameter and has none of its
		// own, and instantiating it keeps that.
		panic(function) // unexpected receiver
	}
	return function
}

func changeReceiver(signature *types.Signature, receiver *types.Var) *types.Signature {
	return types.NewSignatureType(receiver, nil, nil, signature.Params(), signature.Results(), signature.Variadic())
}

// A local version of *types.Selection.
// Needed for some additional control, such as creating a MethodExpr for an instantiation.
type selection struct {
	kind          types.SelectionKind
	receiverType  types.Type
	selectionType types.Type
	object        types.Object
	index         []int
	indirect      bool
}

func toSelection(typesSelection *types.Selection) *selection {
	return &selection{
		kind:          typesSelection.Kind(),
		receiverType:  typesSelection.Recv(),
		selectionType: typesSelection.Type(),
		object:        typesSelection.Obj(),
		index:         typesSelection.Index(),
		indirect:      typesSelection.Indirect(),
	}
}

// -- instantiations --------------------------------------------------
