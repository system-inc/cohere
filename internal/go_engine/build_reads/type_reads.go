package build_reads

import "go/types"

// typeReads are the type reads of one package as its walk collects them, on the walk's own goroutine, and publishes
// them to its Reads under one hold of the lock (see Reads.publishTypes).
type typeReads struct {
	objects map[types.Object]bool
}

// collectTypeReads is every object of another package that info, own's type information, reaches: each
// identifier's use, each embedded field on a selection's path, and each implicit object. A selection's selected field
// or method is among the uses already, since the type checker records a selector's name as a use of what it selects.
// An object of an instance, a field or method of an instantiated type or an instantiated function, is collected as its
// generic declares it, which is what another build can find. An object of no package, the universe's, is the same in
// every build.
//
// An instance of another package's generic is no read of its own. Its generic is a use. Its type arguments are types
// the package wrote, which name each type name they hold, or types it inferred from the objects it read, whose closure
// digests reach every named type their types do (#b2t9xmf): `generic.Identity(platform.Get())` infers
// platform.Handle from Get's result, and Get's digest changes with Handle's definition. So every named type of
// another package a type argument reaches is reached by an object read's closure, which the restore-instance
// fixture's test pins, and a census of every instance at cohere's root, loom and a cohere tree found none
// uncovered.
func collectTypeReads(own *types.Package, info *types.Info) typeReads {
	collected := typeReads{objects: map[types.Object]bool{}}
	for _, used := range info.Uses {
		if foreign, isForeign := foreignDeclared(used, own); isForeign {
			collected.objects[foreign] = true
		}
	}
	for _, selection := range info.Selections {
		// Only a selection through an embedded field has a path past what it selects.
		if len(selection.Index()) > 1 {
			collectSelectionPath(collected.objects, own, selection)
		}
	}
	for _, implicit := range info.Implicits {
		if foreign, isForeign := foreignDeclared(implicit, own); isForeign {
			collected.objects[foreign] = true
		}
	}
	return collected
}

// collectSelectionPath adds to objects each embedded field of another package that selection passes through on its
// way to what it selects, which the package never names, so no use holds it: `outer.Value`, where another package's
// Outer embeds an inner struct declaring Value, reads Outer's embedded inner.
//
// Mutates: objects gains each such field
func collectSelectionPath(objects map[types.Object]bool, own *types.Package, selection *types.Selection) {
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
			objects[foreign] = true
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
