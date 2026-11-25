package locks

import (
	"go/token"
	"go/types"
)

// lockerType is sync.Locker's method set, `interface{ Lock(); Unlock() }`, built here rather than looked up, since
// a package need not import sync to declare a lock of its own.
var lockerType = newLockerType()

// newLockerType builds lockerType. x/tools builds it in copylock.go's init.
func newLockerType() *types.Interface {
	nullary := types.NewSignatureType(nil, nil, nil, nil, nil, false)
	return types.NewInterfaceType([]*types.Func{
		types.NewFunc(token.NoPos, nil, "Lock", nullary),
		types.NewFunc(token.NoPos, nil, "Unlock", nullary),
	}, nil).Complete()
}

// Of is the path to a lock contained in typed, nil when it contains none: x/tools' copylock lockPath, less its
// package parameter, which it never reads. It is the one walk of a type's fields for its locks, which copylocks asks
// of every value it judges and the restore from the host renders of another package's unexported field (see
// type_predicates.Observe), so the two can never disagree.
func Of(typed types.Type) Path {
	return within(typed, map[types.Type]bool{})
}

// within is Of for a type met inside one already being walked, with seen holding each type met so
// far, so a type that contains itself, which only code that does not type-check declares, ends the walk rather than
// recursing forever.
//
// Mutates: seen recording typed as met, since the walk of every type typed holds shares the one map.
func within(typed types.Type, seen map[types.Type]bool) Path {
	if typed == nil || seen[typed] {
		return nil
	}
	seen[typed] = true

	if typeParameter, isTypeParameter := types.Unalias(typed).(*types.TypeParam); isTypeParameter {
		// A type parameter holds a lock when a type its constraint permits does. An invalid constraint, an empty
		// type set and a constraint permitting every type all have no terms.
		for _, term := range structuralTerms(typeParameter) {
			path := within(term.Type(), seen)
			if len(path) == 0 {
				continue
			}
			if term.Tilde() {
				// The tilde makes the message say the containment is through an approximation element. Without it,
				// `func _[Mutex interface{ ~sync.Mutex; M() }](mutex Mutex) {}` would read "Mutex contains
				// sync.Mutex", when sync.Mutex lacks M and is no Mutex.
				path[len(path)-1] = "~" + path[len(path)-1]
			}
			return append(path, typed.String())
		}
		return nil
	}

	// An array copies its elements, so an array of locks is judged as its element, and the path names the element.
	element := typed
	for {
		array, isArray := element.Underlying().(*types.Array)
		if !isArray {
			break
		}
		element = array.Elem()
	}

	// A tuple is the type of a comma-ok read, `value, present := table[key]`, and copies each of its values.
	if tuple, isTuple := element.Underlying().(*types.Tuple); isTuple {
		for variable := range tuple.Variables() {
			if path := within(variable.Type(), seen); path != nil {
				return append(path, element.String())
			}
		}
		return nil
	}

	// Only a struct can hold a lock by value: an interface or a pointer is safe to copy.
	structure, isStructure := element.Underlying().(*types.Struct)
	if !isStructure {
		return nil
	}

	// A lock is a type whose pointer is a sync.Locker and whose value is not, which tells a struct embedding a lock
	// by value from one embedding an interface.
	if types.Implements(types.NewPointer(element), lockerType) && !types.Implements(element, lockerType) {
		return Path{element.String()}
	}

	// Before Go 1.11 sync.noCopy had no Unlock method, so it is recognized by name too. The original keeps the
	// workaround, so the port does, though every toolchain cohere supports gives noCopy both methods.
	if isTypeNamed(element, "sync", "noCopy") {
		return Path{element.String()}
	}

	for index := range structure.NumFields() {
		if path := within(structure.Field(index).Type(), seen); path != nil {
			return append(path, element.String())
		}
	}
	return nil
}

// isTypeNamed reports whether typed is, or is an alias of, the package-level defined type name declares in the
// package at packagePath. x/tools' typesinternal.IsTypeNamed, with one name rather than several.
func isTypeNamed(typed types.Type, packagePath string, name string) bool {
	named, isNamed := types.Unalias(typed).(*types.Named)
	if !isNamed {
		return false
	}
	typeName := named.Obj()
	// x/tools' typesinternal.IsPackageLevel: the name is declared in its package's own scope.
	return typeName != nil && typeName.Pkg() != nil && typeName.Parent() == typeName.Pkg().Scope() &&
		typeName.Pkg().Path() == packagePath && typeName.Name() == name
}
