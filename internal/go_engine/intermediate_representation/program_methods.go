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
// prog.CreatePackage.
//
// objectMethod panics if the function is not a method.
//
// Acquires prog.objectMethodsMu.
func (prog *Program) objectMethod(object *types.Func, targs []types.Type, builder *builder) *Function {
	sig := object.Type().(*types.Signature)
	if sig.Recv() == nil {
		panic("not a method: " + object.String())
	}

	// Instantiation of generic?
	if orig := object.Origin(); orig != object || len(targs) > 0 {
		return prog.objectMethod(orig, nil, builder).instance(receiverTypeArguments(object), targs, builder)
	}

	// Belongs to a created package?
	if function := prog.FunctionValue(object); function != nil {
		return function
	}

	// Consult/update cache of methods created from types.Func.
	prog.objectMethodsMutex.Lock()
	defer prog.objectMethodsMutex.Unlock()
	function, ok := prog.objectMethods[object]
	if !ok {
		function = createFunction(prog, object, object.Name(), nil, nil, "")
		function.Synthetic = "from type information (on demand)"
		function.buildshared = builder.shared()
		builder.enqueue(function)

		if prog.objectMethods == nil {
			prog.objectMethods = make(map[*types.Func]*Function)
		}
		prog.objectMethods[object] = function
	} else {
		builder.waitForSharedFunction(function)
	}
	return function
}
