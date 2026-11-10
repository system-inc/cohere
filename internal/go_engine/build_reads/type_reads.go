package build_reads

import (
	"go/types"
	"slices"
)

// typeReads are the type reads of one package as its walk collects them, on the walk's own goroutine, and publishes
// them to its Reads under one hold of the lock (see Reads.publishTypes).
type typeReads struct {
	objects   map[types.Object]bool
	instances map[types.Object][]*types.TypeList
}

// collectTypeReads is every object of another package that info, own's type information, reaches: each
// identifier's use, each embedded field on a selection's path, each implicit object, and, for each instance of
// another package's generic, its type arguments, each distinct list once. A selection's selected field or method is
// among the uses already, since the type checker records a selector's name as a use of what it selects. An object of
// an instance, a field or method of an instantiated type or an instantiated function, is collected as its generic
// declares it, which is what another build can find; the instance's type arguments are its own read. An object of no
// package, the universe's, is the same in every build.
func collectTypeReads(own *types.Package, info *types.Info) typeReads {
	collected := typeReads{objects: map[types.Object]bool{}, instances: map[types.Object][]*types.TypeList{}}
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
	for identifier, instance := range info.Instances {
		generic, isForeign := foreignDeclared(info.Uses[identifier], own)
		if !isForeign || slices.ContainsFunc(collected.instances[generic], func(recorded *types.TypeList) bool {
			return identicalLists(recorded, instance.TypeArgs)
		}) {
			continue
		}
		collected.instances[generic] = append(collected.instances[generic], instance.TypeArgs)
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
