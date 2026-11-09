package intermediate_representation

import (
	"fmt"
	"go/types"
	"sync"

	"honnef.co/go/tools/go/types/typeutil"
)

// Mapping of a type T to a canonical instance C s.t. types.Identical(T, C).
// Thread-safe.
type canonizer struct {
	mutex sync.Mutex
	types typeutil.Map[types.Type] // map from type to a canonical instance
	lists typeListMap              // map from a list of types to a canonical instance
}

// List returns a canonical representative of a list of types.
// Representative of the empty list is nil.
func (canonizer *canonizer) List(ts []types.Type) *typeList {
	if len(ts) == 0 {
		return nil
	}

	unaliasAll := func(ts []types.Type) []types.Type {
		// Is there some top level alias?
		var found bool
		for _, resolvedType := range ts {
			if _, ok := resolvedType.(*types.Alias); ok {
				found = true
				break
			}
		}
		if !found {
			return ts // no top level alias
		}

		cp := make([]types.Type, len(ts)) // copy with top level aliases removed.
		for index, resolvedType := range ts {
			cp[index] = types.Unalias(resolvedType)
		}
		return cp
	}
	resolvedTypes := unaliasAll(ts)

	canonizer.mutex.Lock()
	defer canonizer.mutex.Unlock()
	return canonizer.lists.rep(resolvedTypes)
}

// Type returns a canonical representative of type T.
// Removes top-level aliases.
//
// For performance, reasons the canonical instance is order-dependent,
// and may contain deeply nested aliases.
func (canonizer *canonizer) Type(goType types.Type) types.Type {
	unaliased := types.Unalias(goType) // remove the top level alias.

	canonizer.mutex.Lock()
	defer canonizer.mutex.Unlock()

	if resolvedType, _ := canonizer.types.At(unaliased); resolvedType != nil {
		return resolvedType
	}
	canonizer.types.Set(unaliased, unaliased)
	return unaliased
}

// instantiateMethod instantiates `function` with targs and returns a canonical representative for this method.
func (canonizer *canonizer) instantiateMethod(
	function *types.Func,
	targs []types.Type,
	ctxt *types.Context,
) *types.Func {
	recv := recvType(function)
	if pointer, ok := types.Unalias(recv).(*types.Pointer); ok {
		recv = pointer.Elem()
	}
	named, isNamed := types.Unalias(recv).(*types.Named)
	if !isNamed {
		// Invariant: callers instantiate only a method with receiver type arguments, which is declared on a generic
		// named type, so its receiver is that type or a pointer to it.
		panic(fmt.Sprintf("instantiateMethod: %s has receiver %s, not a named type", function, recv))
	}
	inst, instantiateError := types.Instantiate(ctxt, named.Origin(), targs, false)
	if instantiateError != nil {
		// Invariant: with validate false, go/types documents that Instantiate never returns an error.
		panic(instantiateError)
	}
	rep := canonizer.Type(inst)
	object, _, _ := types.LookupFieldOrMethod(rep, true, function.Pkg(), function.Name())
	method, isMethod := object.(*types.Func)
	if !isMethod {
		// Invariant: an instance of a named type has every method of its origin, function among them.
		panic(fmt.Sprintf("instantiateMethod: %s has no method %s", rep, function.Name()))
	}
	return method
}
