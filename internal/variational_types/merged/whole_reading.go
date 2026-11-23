package merged

import (
	"github.com/system-inc/go-types/types"
)

// wholeReading is what a whole plain importer has read whole so far, under its build's lock: every type it walked, so
// a type reached again is walked once.
type wholeReading struct {
	walked map[types.Type]bool
}

// readPackage reads imported whole: every object of its scope, and every type each reaches, through to the
// declaration of every named type and the methods of every instance. A plain universe reads an object the first time
// it is looked up and a named type's declaration the first time it is needed, sharing the universe's state and its
// readers' caches with no lock of their own, so checks that run at once would read export data at once. Read whole
// at its import, under the build's lock, a package leaves its checks only decoded types to read, which are safe to read
// at once as go/types' own are.
//
// Requires: the build's lock is held.
func (whole *wholeReading) readPackage(imported *types.Package) {
	scope := imported.Scope()
	for _, name := range scope.Names() {
		whole.readType(scope.Lookup(name).Type())
	}
}

// readType reads reachedType and every type it reaches, each once.
//
// Requires: the build's lock is held.
func (whole *wholeReading) readType(reachedType types.Type) {
	if reachedType == nil || whole.walked[reachedType] {
		return
	}
	whole.walked[reachedType] = true
	switch concreteType := reachedType.(type) {
	case *types.Basic:
		// A basic type reaches nothing.
	case *types.Pointer:
		whole.readType(concreteType.Elem())
	case *types.Slice:
		whole.readType(concreteType.Elem())
	case *types.Array:
		whole.readType(concreteType.Elem())
	case *types.Map:
		whole.readType(concreteType.Key())
		whole.readType(concreteType.Elem())
	case *types.Chan:
		whole.readType(concreteType.Elem())
	case *types.Struct:
		for index := range concreteType.NumFields() {
			whole.readType(concreteType.Field(index).Type())
		}
	case *types.Tuple:
		// Len and At take a nil tuple, a signature's empty parameters or results.
		for index := range concreteType.Len() {
			whole.readType(concreteType.At(index).Type())
		}
	case *types.Signature:
		if receiver := concreteType.Recv(); receiver != nil {
			whole.readType(receiver.Type())
		}
		whole.readTypeParameters(concreteType.TypeParams())
		whole.readTypeParameters(concreteType.RecvTypeParams())
		whole.readType(concreteType.Params())
		whole.readType(concreteType.Results())
	case *types.Interface:
		// Its type set is computed now if the reader left it to its first use.
		for index := range concreteType.NumMethods() {
			whole.readType(concreteType.Method(index).Type())
		}
		for index := range concreteType.NumEmbeddeds() {
			whole.readType(concreteType.EmbeddedType(index))
		}
	case *types.Union:
		for index := range concreteType.Len() {
			whole.readType(concreteType.Term(index).Type())
		}
	case *types.TypeParam:
		whole.readType(concreteType.Constraint())
	case *types.Alias:
		whole.readTypeParameters(concreteType.TypeParams())
		whole.readTypeList(concreteType.TypeArgs())
		whole.readType(concreteType.Origin())
		whole.readType(concreteType.Rhs())
	case *types.Named:
		// The declaration is read here, by the named type's loader, and an instance's underlying type and methods are
		// expanded.
		whole.readTypeParameters(concreteType.TypeParams())
		whole.readTypeList(concreteType.TypeArgs())
		whole.readType(concreteType.Origin())
		whole.readType(concreteType.Underlying())
		for index := range concreteType.NumMethods() {
			whole.readType(concreteType.Method(index).Type())
		}
	}
}

// readTypeParameters reads each of parameters' constraints.
//
// Requires: the build's lock is held.
func (whole *wholeReading) readTypeParameters(parameters *types.TypeParamList) {
	for index := range parameters.Len() {
		whole.readType(parameters.At(index))
	}
}

// readTypeList reads each of list's types.
//
// Requires: the build's lock is held.
func (whole *wholeReading) readTypeList(list *types.TypeList) {
	for index := range list.Len() {
		whole.readType(list.At(index))
	}
}
