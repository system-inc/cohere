package intermediate_representation

import "go/types"

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

// buildInstantiationWrapper builds the body of an instantiation
// wrapper fn. The body calls the original generic function,
// bracketed by ChangeType conversions on its arguments and results.
//
// Mutates: function gains its body, which ends in the return of the call's results
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
