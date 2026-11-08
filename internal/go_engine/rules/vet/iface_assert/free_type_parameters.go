package iface_assert

import "go/types"

// freeTypeParameters remembers which types hold a free type parameter, for a sequence of questions about types that
// overlap. The zero value is ready. x/tools' typeparams.Free, which adapts go/types' infer.go.
type freeTypeParameters struct {
	// seen holds each type asked about, and while its answer is being computed, false, so a type that holds itself
	// answers false within itself, as x/tools' does. Kept across questions, that answer can stand for a type met
	// inside its own computation, so the port asks in the original's order.
	seen map[types.Type]bool
}

// has is whether searched holds a free type parameter. x/tools' typeparams.Free.Has.
func (free *freeTypeParameters) has(searched types.Type) (found bool) {
	if remembered, isSeen := free.seen[searched]; isSeen {
		return remembered
	}
	if free.seen == nil {
		free.seen = map[types.Type]bool{}
	}
	free.seen[searched] = false
	defer func() {
		free.seen[searched] = found
	}()

	switch typed := searched.(type) {
	case nil, *types.Basic:
		return false
	case *types.Alias:
		if typed.TypeParams().Len() > typed.TypeArgs().Len() {
			// An alias with fewer arguments than parameters is not instantiated.
			return true
		}
		// What an alias stands for can hold a free type parameter whether or not the alias has parameters of its
		// own, as `type Set = map[K]bool` declared inside a function generic in K does.
		return free.has(types.Unalias(typed))
	case *types.Array:
		return free.has(typed.Elem())
	case *types.Slice:
		return free.has(typed.Elem())
	case *types.Struct:
		for index := range typed.NumFields() {
			if free.has(typed.Field(index).Type()) {
				return true
			}
		}
		return false
	case *types.Pointer:
		return free.has(typed.Elem())
	case *types.Tuple:
		for index := range typed.Len() {
			if free.has(typed.At(index).Type()) {
				return true
			}
		}
		return false
	case *types.Signature:
		// A signature's own type parameters, of a generic function or an interface method, and a receiver's, which
		// a method declares rather than uses, are not free, so only its parameters and results are searched.
		return free.has(typed.Params()) || free.has(typed.Results())
	case *types.Interface:
		for index := range typed.NumMethods() {
			if free.has(typed.Method(index).Type()) {
				return true
			}
		}
		terms, termsError := interfaceTermSet(typed)
		if termsError != nil {
			// An interface whose term set cannot be computed, or holds no type, answers false, as x/tools' does.
			return false
		}
		for _, member := range terms {
			if free.has(member.termType) {
				return true
			}
		}
		return false
	case *types.Map:
		return free.has(typed.Key()) || free.has(typed.Elem())
	case *types.Chan:
		return free.has(typed.Elem())
	case *types.Named:
		arguments := typed.TypeArgs()
		if typed.TypeParams().Len() > arguments.Len() {
			// A named type with fewer arguments than parameters is not instantiated.
			return true
		}
		for index := range arguments.Len() {
			if free.has(arguments.At(index)) {
				return true
			}
		}
		// A type declared inside a generic function can hold the function's type parameters in its underlying type.
		return free.has(typed.Underlying())
	case *types.TypeParam:
		return true
	default:
		// Invariant: the search reaches only the types above. A union is the one other type go/types makes, and it
		// sits only among an interface's embedded elements, which the search reads through the term set instead.
		panic("iface_assert: a free type parameter searched for in an unknown type")
	}
}
