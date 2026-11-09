// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"fmt"
	"go/types"
	"sync"
)

// A generic records information about a generic origin function,
// including a cache of existing instantiations.
type generic struct {
	instancesMutex sync.Mutex
	instances      map[*typeList]*Function // canonical type arguments to an instance.
}

// createInstance returns the instantiation of generic function fn using targs.
// If fn is a method on a generic type, fn's receiver type will be instantiated
// using rtargs.
//
// Requires fn.generic.instancesMu.
func createInstance(function *Function, rtargs, targs []types.Type) *Function {
	prog := function.Program

	// Compute signature.
	var sig *types.Signature
	var object *types.Func
	if recv := function.Signature.Recv(); recv != nil {
		// method, len(rtargs) > 0 || len(targs) > 0
		if len(rtargs) > 0 {
			// possibly generic method on generic type
			object = prog.canon.instantiateMethod(function.object, rtargs, prog.ctxt)
		} else {
			// generic method on non-generic type
			object = function.object // instantiation does not exist yet
		}
		if len(targs) > 0 {
			// generic method
			instSig, instantiateError := types.Instantiate(prog.ctxt, object.Signature(), targs, false)
			if instantiateError != nil {
				panic(instantiateError)
			}
			instance, ok := instSig.(*types.Signature)
			if !ok {
				panic("Instantiate of a Signature returned a non-signature")
			}
			// Do not canonicalize generic methods, because the receiver is not
			// part of a Signature's type identity. For example,
			// (*G[int]).m[int] and (G[int]).n[int] may be identical types even
			// though their receiver types differ.
			sig = instance
		} else {
			// non-generic method on generic type
			sig = object.Signature()
		}
	} else {
		// function, len(rtargs) == 0 && len(targs) > 0
		instSig, instantiateError := types.Instantiate(prog.ctxt, function.Signature, targs, false)
		if instantiateError != nil {
			panic(instantiateError)
		}
		instance, ok := instSig.(*types.Signature)
		if !ok {
			panic("Instantiate of a Signature returned a non-signature")
		}
		object = function.object // instantiation does not exist yet
		sig = prog.canon.Type(instance).(*types.Signature)
	}

	// Always a wrapper. go/ir builds an instance from the generic body with its type arguments
	// substituted when its InstantiateGenerics mode is set; ours builds generic bodies in generic
	// form only, as go/ir does without that mode.
	synthetic := fmt.Sprintf("instantiation wrapper of %s", function.Name())
	build := (*builder).buildInstantiationWrapper

	name := function.Name()
	if len(targs) > 0 {
		name = fmt.Sprintf("%s%s", name, targstr(targs)) // may not be unique
	}

	/* generic instance or instantiation wrapper */
	ret := &Function{
		name:           name,
		object:         object,
		Signature:      sig,
		Synthetic:      synthetic,
		syntax:         function.syntax,    // \
		info:           function.info,      //  } empty for non-created packages
		goversion:      function.goversion, // /
		build:          build,
		topLevelOrigin: function,
		position:       object.Pos(),
		Package:        nil,
		Program:        function.Program,
		recvtypeparams: function.recvtypeparams, // share with origin
		recvtypeargs:   rtargs,
		typeparams:     function.typeparams, // share with origin
		typeargs:       targs,
	}
	return ret
}
