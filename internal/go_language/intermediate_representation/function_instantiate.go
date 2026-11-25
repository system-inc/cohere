package intermediate_representation

import (
	"go/types"
	"slices"
)

// instance returns a Function that is the instantiation of generic
// origin function fn with the type arguments rtargs and targs.
//
// Any created instance is added to cr.
//
// Acquires fn.generic.instancesMu.
func (function *Function) instance(rtargs, targs []types.Type, builder *builder) *Function {
	key := function.Program.canon.List(slices.Concat(rtargs, targs))

	gen := function.generic

	gen.instancesMutex.Lock()
	defer gen.instancesMutex.Unlock()
	inst, ok := gen.instances[key]
	if !ok {
		inst = createInstance(function, rtargs, targs)
		inst.buildshared = builder.shared()
		builder.enqueue(inst)

		if gen.instances == nil {
			gen.instances = make(map[*typeList]*Function)
		}
		gen.instances[key] = inst
	} else {
		builder.waitForSharedFunction(inst)
	}
	return inst
}
