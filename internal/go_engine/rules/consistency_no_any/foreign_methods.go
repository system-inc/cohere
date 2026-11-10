package consistency_no_any

import "go/types"

// foreignMethods are the exported methods of every named type in a package's import graph, interfaces and concrete
// types alike, by name. Signature identity ignores receivers, so each is matched by its signature alone, and held as
// the method, which a recorded answer names.
type foreignMethods struct {
	byName map[string][]*types.Func
	// genericByName holds a generic type's methods apart, since their signatures speak of type parameters that only
	// an instantiation the rule cannot know replaces, so they are matched by shape.
	genericByName map[string][]*types.Func
}

// add indexes named's exported methods: an interface's whole method set, embedded methods included, and a concrete
// type's own declared methods. Unexported ones belong to their package, where no other package can match them.
func (foreign *foreignMethods) add(named *types.Named) {
	index := foreign.byName
	if named.TypeParams().Len() > 0 {
		index = foreign.genericByName
	}
	if interfaceType, isInterface := named.Underlying().(*types.Interface); isInterface {
		for methodIndex := range interfaceType.NumMethods() {
			method := interfaceType.Method(methodIndex)
			if method.Exported() {
				index[method.Name()] = append(index[method.Name()], method)
			}
		}
		return
	}
	for methodIndex := range named.NumMethods() {
		method := named.Method(methodIndex)
		if method.Exported() {
			index[method.Name()] = append(index[method.Name()], method)
		}
	}
}
