// Copyright 2022 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package intermediate_representation

import (
	"fmt"
	"go/types"
	"slices"
	"sync"
)

// A generic records information about a generic origin function,
// including a cache of existing instantiations.
type generic struct {
	instancesMu sync.Mutex
	instances   map[*typeList]*Function // canonical type arguments to an instance.
}

// instance returns a Function that is the instantiation of generic
// origin function fn with the type arguments rtargs and targs.
//
// Any created instance is added to cr.
//
// Acquires fn.generic.instancesMu.
func (fn *Function) instance(rtargs, targs []types.Type, b *builder) *Function {
	key := fn.Program.canon.List(slices.Concat(rtargs, targs))

	gen := fn.generic

	gen.instancesMu.Lock()
	defer gen.instancesMu.Unlock()
	inst, ok := gen.instances[key]
	if !ok {
		inst = createInstance(fn, rtargs, targs)
		inst.buildshared = b.shared()
		b.enqueue(inst)

		if gen.instances == nil {
			gen.instances = make(map[*typeList]*Function)
		}
		gen.instances[key] = inst
	} else {
		b.waitForSharedFunction(inst)
	}
	return inst
}

// createInstance returns the instantiation of generic function fn using targs.
// If fn is a method on a generic type, fn's receiver type will be instantiated
// using rtargs.
//
// Requires fn.generic.instancesMu.
func createInstance(fn *Function, rtargs, targs []types.Type) *Function {
	prog := fn.Program

	// Compute signature.
	var sig *types.Signature
	var obj *types.Func
	if recv := fn.Signature.Recv(); recv != nil {
		// method, len(rtargs) > 0 || len(targs) > 0
		if len(rtargs) > 0 {
			// possibly generic method on generic type
			obj = prog.canon.instantiateMethod(fn.object, rtargs, prog.ctxt)
		} else {
			// generic method on non-generic type
			obj = fn.object // instantiation does not exist yet
		}
		if len(targs) > 0 {
			// generic method
			instSig, err := types.Instantiate(prog.ctxt, obj.Signature(), targs, false)
			if err != nil {
				panic(err)
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
			sig = obj.Signature()
		}
	} else {
		// function, len(rtargs) == 0 && len(targs) > 0
		instSig, err := types.Instantiate(prog.ctxt, fn.Signature, targs, false)
		if err != nil {
			panic(err)
		}
		instance, ok := instSig.(*types.Signature)
		if !ok {
			panic("Instantiate of a Signature returned a non-signature")
		}
		obj = fn.object // instantiation does not exist yet
		sig = prog.canon.Type(instance).(*types.Signature)
	}

	// Always a wrapper. go/ir builds an instance from the generic body with its type arguments
	// substituted when its InstantiateGenerics mode is set; ours builds generic bodies in generic
	// form only, as go/ir does without that mode.
	synthetic := fmt.Sprintf("instantiation wrapper of %s", fn.Name())
	build := (*builder).buildInstantiationWrapper

	name := fn.Name()
	if len(targs) > 0 {
		name = fmt.Sprintf("%s%s", name, targstr(targs)) // may not be unique
	}

	/* generic instance or instantiation wrapper */
	ret := &Function{
		name:           name,
		object:         obj,
		Signature:      sig,
		Synthetic:      synthetic,
		syntax:         fn.syntax,    // \
		info:           fn.info,      //  } empty for non-created packages
		goversion:      fn.goversion, // /
		build:          build,
		topLevelOrigin: fn,
		pos:            obj.Pos(),
		Package:        nil,
		Program:        fn.Program,
		recvtypeparams: fn.recvtypeparams, // share with origin
		recvtypeargs:   rtargs,
		typeparams:     fn.typeparams, // share with origin
		typeargs:       targs,
	}
	return ret
}
