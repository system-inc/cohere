// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

// This file defines utilities for population of method sets.

import (
	"go/types"
)

// objectMethod returns the Function for a given method symbol.
// The symbol may be an instance of a generic function. It need not
// belong to an existing SSA package created by a call to
// program.CreatePackage.
//
// objectMethod panics if the function is not a method.
//
// Acquires program.objectMethodsMutex.
func (program *Program) objectMethod(object *types.Func, targs []types.Type, builder *builder) *Function {
	sig, isSignature := object.Type().(*types.Signature)
	if !isSignature {
		// Invariant: go/types gives every *types.Func a *types.Signature type.
		panic("not a function: " + object.String())
	}
	if sig.Recv() == nil {
		// Invariant: callers pass the object of a method selection or of a method wrapper, each with a receiver.
		panic("not a method: " + object.String())
	}

	// Instantiation of generic?
	if orig := object.Origin(); orig != object || len(targs) > 0 {
		return program.objectMethod(orig, nil, builder).instance(receiverTypeArguments(object), targs, builder)
	}

	// Belongs to a created package?
	if function := program.FunctionValue(object); function != nil {
		return function
	}

	// Consult/update cache of methods created from types.Func.
	program.objectMethodsMutex.Lock()
	defer program.objectMethodsMutex.Unlock()
	function, ok := program.objectMethods[object]
	if !ok {
		function = createFunction(program, object, object.Name(), nil, nil, "")
		function.Synthetic = "from type information (on demand)"
		function.buildshared = builder.shared()
		builder.enqueue(function)

		if program.objectMethods == nil {
			program.objectMethods = make(map[*types.Func]*Function)
		}
		program.objectMethods[object] = function
	} else {
		builder.waitForSharedFunction(function)
	}
	return function
}
