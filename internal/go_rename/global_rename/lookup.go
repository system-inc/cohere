package global_rename

import (
	"go/token"
	"go/types"
)

// nameOf gives the name an object has in the world being modeled: its own name, or its name once the
// accepted renames are applied.
type nameOf func(types.Object) string

// member is the result of looking a name up in a type's fields and methods.
type member struct {
	// object is the field or method found, nil when there is none or the lookup is ambiguous.
	object types.Object
	// collisions holds every object matched at the shallowest depth when more than one was.
	collisions []types.Object
	// pointerReceiverRequired is set when the only match is a pointer method a value's method set lacks.
	pointerReceiverRequired bool
}

// embeddedEntry is one type searched at a depth of the embedding tree.
type embeddedEntry struct {
	goType    types.Type
	indirect  bool
	multiples bool
}

// lookupMember mirrors go/types' LookupFieldOrMethod with names given by name: the shallowest depth holding
// a field or method of that name wins, two at that depth are a collision, a method set needs a pointer for a
// pointer method unless addressable, and an unexported name matches only in its own package. go/types cannot
// be asked to look up a name an object does not have yet, so the search is redone here; the engine checks it
// against every selection go/types recorded, under the old names, before trusting it under the new.
func lookupMember(receiver types.Type, addressable bool, from *types.Package, wanted string, name nameOf) member {
	matches := func(object types.Object) bool {
		if name(object) != wanted {
			return false
		}
		if token.IsExported(wanted) {
			return true
		}
		return object.Pkg() != nil && from != nil && object.Pkg().Path() == from.Path()
	}
	start, isPointer := dereference(receiver)
	if isPointer && types.IsInterface(start) {
		return member{}
	}
	current := []embeddedEntry{{goType: start, indirect: isPointer}}
	var seen []*types.Named
	for len(current) > 0 {
		var next []embeddedEntry
		var found []types.Object
		indirect := false
		for _, entry := range current {
			if named, isNamed := types.Unalias(entry.goType).(*types.Named); isNamed {
				if containsIdentical(seen, named) {
					continue
				}
				seen = append(seen, named)
				matchedMethod := false
				for index := range named.NumMethods() {
					method := named.Method(index)
					if matches(method) {
						found = append(found, method)
						if entry.multiples {
							found = append(found, method)
						}
						indirect = entry.indirect
						matchedMethod = true
					}
				}
				if matchedMethod {
					continue
				}
			}
			switch underlying := entry.goType.Underlying().(type) {
			case *types.Struct:
				for index := range underlying.NumFields() {
					field := underlying.Field(index)
					if matches(field) {
						found = append(found, field)
						if entry.multiples {
							found = append(found, field)
						}
						indirect = entry.indirect
						continue
					}
					if len(found) == 0 && field.Embedded() {
						fieldType, fieldIsPointer := dereference(field.Type())
						next = append(next, embeddedEntry{
							goType:    fieldType,
							indirect:  entry.indirect || fieldIsPointer,
							multiples: entry.multiples,
						})
					}
				}
			case *types.Interface:
				for index := range underlying.NumMethods() {
					method := underlying.Method(index)
					if matches(method) {
						found = append(found, method)
						if entry.multiples {
							found = append(found, method)
						}
						indirect = entry.indirect
					}
				}
			}
		}
		if len(found) == 1 {
			if function, isFunction := found[0].(*types.Func); isFunction &&
				hasPointerReceiver(function) &&
				!indirect &&
				!addressable {
				return member{pointerReceiverRequired: true}
			}
			return member{object: found[0]}
		}
		if len(found) > 1 {
			return member{collisions: found}
		}
		current = consolidate(next)
	}
	return member{}
}

// dereference is a pointer's element type, as go/types' deref: only a pointer type literal, never a named one.
func dereference(goType types.Type) (types.Type, bool) {
	if pointer, isPointer := types.Unalias(goType).(*types.Pointer); isPointer {
		return pointer.Elem(), true
	}
	return goType, false
}

func hasPointerReceiver(function *types.Func) bool {
	receiver := function.Signature().Recv()
	if receiver == nil {
		return false
	}
	_, isPointer := types.Unalias(receiver.Type()).(*types.Pointer)
	return isPointer
}

func containsIdentical(seen []*types.Named, named *types.Named) bool {
	for _, previous := range seen {
		if types.Identical(previous, named) {
			return true
		}
	}
	return false
}

// consolidate merges the entries of one depth that hold the same type, marking them multiples: a name found
// through such a type is ambiguous.
func consolidate(entries []embeddedEntry) []embeddedEntry {
	var merged []embeddedEntry
	for _, entry := range entries {
		duplicate := false
		for index := range merged {
			if types.Identical(merged[index].goType, entry.goType) {
				merged[index].multiples = true
				duplicate = true
				break
			}
		}
		if !duplicate {
			merged = append(merged, entry)
		}
	}
	return merged
}

// memberNames lists every field and method name reachable from goType through embedding, under name.
//
// Mutates: into the caller's set of names, which gathers the members of every type it is called with
func memberNames(goType types.Type, name nameOf, into map[string]bool) {
	start, _ := dereference(goType)
	pending := []types.Type{start}
	var seen []*types.Named
	visitedStructs := map[*types.Struct]bool{}
	for len(pending) > 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if named, isNamed := types.Unalias(current).(*types.Named); isNamed {
			if containsIdentical(seen, named) {
				continue
			}
			seen = append(seen, named)
			for index := range named.NumMethods() {
				into[name(named.Method(index))] = true
			}
		}
		switch underlying := current.Underlying().(type) {
		case *types.Struct:
			if visitedStructs[underlying] {
				continue
			}
			visitedStructs[underlying] = true
			for index := range underlying.NumFields() {
				field := underlying.Field(index)
				into[name(field)] = true
				if field.Embedded() {
					fieldType, _ := dereference(field.Type())
					pending = append(pending, fieldType)
				}
			}
		case *types.Interface:
			for index := range underlying.NumMethods() {
				into[name(underlying.Method(index))] = true
			}
		}
	}
}

// satisfies reports whether goType's method set has every method of the interface, with an identical
// signature, when names are given by name. Only methods count: a type set's terms are not names.
func satisfies(goType types.Type, interfaceType *types.Interface, name nameOf) bool {
	for index := range interfaceType.NumMethods() {
		wanted := interfaceType.Method(index)
		found := lookupMember(goType, false, wanted.Pkg(), name(wanted), name)
		function, isFunction := found.object.(*types.Func)
		if !isFunction || !types.Identical(function.Type(), wanted.Type()) {
			return false
		}
	}
	return true
}

// satisfiesInShape is the rules circle's reading of a generic interface, which only an instantiation
// implements: goType has every method of it with as many parameters and results and the same variadic ending.
func satisfiesInShape(goType types.Type, interfaceType *types.Interface, name nameOf) bool {
	for index := range interfaceType.NumMethods() {
		wanted := interfaceType.Method(index)
		found := lookupMember(goType, false, wanted.Pkg(), name(wanted), name)
		function, isFunction := found.object.(*types.Func)
		if !isFunction {
			return false
		}
		if function.Signature().Params().Len() != wanted.Signature().Params().Len() ||
			function.Signature().Results().Len() != wanted.Signature().Results().Len() ||
			function.Signature().Variadic() != wanted.Signature().Variadic() {
			return false
		}
	}
	return true
}
