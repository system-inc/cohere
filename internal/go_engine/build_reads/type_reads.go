package build_reads

import "go/types"

// recordTypeReads records into reads every object of another package that info reaches, own's type information: each
// identifier's use, each selection's selected field or method and the embedded fields on its path, each implicit
// object, and, for each instance of another package's generic, its type arguments. An object of an instance, a field
// or method of an instantiated type or an instantiated function, is recorded as its generic declares it, which is
// what another build can find; the instance's type arguments are its own read. An object of no package, the
// universe's, is the same in every build.
//
// Mutates: reads gains every read
func recordTypeReads(reads *Reads, own *types.Package, info *types.Info) {
	for _, used := range info.Uses {
		if foreign, isForeign := foreignDeclared(used, own); isForeign {
			reads.AddObject(foreign)
		}
	}
	for _, selection := range info.Selections {
		if foreign, isForeign := foreignDeclared(selection.Obj(), own); isForeign {
			reads.AddObject(foreign)
		}
		recordSelectionPath(reads, own, selection)
	}
	for _, implicit := range info.Implicits {
		if foreign, isForeign := foreignDeclared(implicit, own); isForeign {
			reads.AddObject(foreign)
		}
	}
	for identifier, instance := range info.Instances {
		if generic, isForeign := foreignDeclared(info.Uses[identifier], own); isForeign {
			reads.AddInstance(generic, instance.TypeArgs)
		}
	}
}

// recordSelectionPath records into reads each embedded field of another package that selection passes through on its
// way to what it selects, which the package never names, so no use holds it: `outer.Value`, where another package's
// Outer embeds an inner struct declaring Value, reads Outer's embedded inner.
//
// Mutates: reads gains each such field
func recordSelectionPath(reads *Reads, own *types.Package, selection *types.Selection) {
	path := selection.Index()
	current := selection.Recv()
	for _, index := range path[:len(path)-1] {
		if pointer, isPointer := current.Underlying().(*types.Pointer); isPointer {
			current = pointer.Elem()
		}
		structure, isStruct := current.Underlying().(*types.Struct)
		if !isStruct {
			return
		}
		embedded := structure.Field(index)
		if foreign, isForeign := foreignDeclared(embedded, own); isForeign {
			reads.AddObject(foreign)
		}
		current = embedded.Type()
	}
}

// foreignDeclared is object as its package declares it, the generic one for a field, method or function of an
// instance, and whether that package is another than own. An object of no package, the universe's, is no other's.
func foreignDeclared(object types.Object, own *types.Package) (types.Object, bool) {
	if object == nil || object.Pkg() == nil || object.Pkg() == own {
		return nil, false
	}
	switch instantiated := object.(type) {
	case *types.Func:
		return instantiated.Origin(), true
	case *types.Var:
		return instantiated.Origin(), true
	}
	return object, true
}
