package merged

import (
	"github.com/system-inc/go-types/types"
)

// typeWalk reaches every type an object's type reaches: through each named type's declaration, its underlying type in
// every build when a merged one differs by build, the methods of every instance and an object's type in every build it
// differs in, each type once as firstVisit says. Reading a plain package whole walks it under its build's lock, and
// warming the merged universe walks it from many goroutines at once.
type typeWalk struct {
	// firstVisit says whether reachedType is walked for the first time, and marks it walked.
	firstVisit func(reachedType types.Type) bool
}

// object walks object's type, and its type in every build when it differs by build.
func (walk typeWalk) object(object types.Object) {
	walk.walk(object.Type())
	for _, variant := range types.VariantsOf(object) {
		walk.walk(variant.Type)
	}
}

// walk reaches reachedType and every type it reaches, each once.
func (walk typeWalk) walk(reachedType types.Type) {
	if reachedType == nil || !walk.firstVisit(reachedType) {
		return
	}
	switch concreteType := reachedType.(type) {
	case *types.Basic:
		// A basic type reaches nothing.
	case *types.Pointer:
		walk.walk(concreteType.Elem())
	case *types.Slice:
		walk.walk(concreteType.Elem())
	case *types.Array:
		walk.walk(concreteType.Elem())
	case *types.Map:
		walk.walk(concreteType.Key())
		walk.walk(concreteType.Elem())
	case *types.Chan:
		walk.walk(concreteType.Elem())
	case *types.Struct:
		for index := range concreteType.NumFields() {
			walk.walk(concreteType.Field(index).Type())
		}
	case *types.Tuple:
		// Len and At take a nil tuple, a signature's empty parameters or results.
		for index := range concreteType.Len() {
			walk.walk(concreteType.At(index).Type())
		}
	case *types.Signature:
		if receiver := concreteType.Recv(); receiver != nil {
			walk.walk(receiver.Type())
		}
		walk.typeParameters(concreteType.TypeParams())
		walk.typeParameters(concreteType.RecvTypeParams())
		walk.walk(concreteType.Params())
		walk.walk(concreteType.Results())
	case *types.Interface:
		// Its type set is computed now if the reader left it to its first use.
		for index := range concreteType.NumMethods() {
			walk.walk(concreteType.Method(index).Type())
		}
		for index := range concreteType.NumEmbeddeds() {
			walk.walk(concreteType.EmbeddedType(index))
		}
	case *types.Union:
		for index := range concreteType.Len() {
			walk.walk(concreteType.Term(index).Type())
		}
	case *types.TypeParam:
		walk.walk(concreteType.Constraint())
	case *types.Alias:
		walk.typeParameters(concreteType.TypeParams())
		walk.typeList(concreteType.TypeArgs())
		walk.walk(concreteType.Origin())
		walk.walk(concreteType.Rhs())
	case *types.Named:
		// The declaration is read here, by the named type's loader, and an instance's underlying type and methods are
		// expanded.
		walk.typeParameters(concreteType.TypeParams())
		walk.typeList(concreteType.TypeArgs())
		walk.walk(concreteType.Origin())
		walk.walk(concreteType.Underlying())
		for _, variant := range types.UnderlyingVariantsOf(concreteType) {
			walk.walk(variant.Underlying)
		}
		for index := range concreteType.NumMethods() {
			walk.object(concreteType.Method(index))
		}
	}
}

// typeParameters walks each of parameters' constraints.
func (walk typeWalk) typeParameters(parameters *types.TypeParamList) {
	for index := range parameters.Len() {
		walk.walk(parameters.At(index))
	}
}

// typeList walks each of list's types.
func (walk typeWalk) typeList(list *types.TypeList) {
	for index := range list.Len() {
		walk.walk(list.At(index))
	}
}
