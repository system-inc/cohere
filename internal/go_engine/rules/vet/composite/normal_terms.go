package composite

import (
	"errors"
	"fmt"
	"go/types"
)

// errorEmptyTypeSet is the error of a constraint no type satisfies. x/tools' typeparams ErrEmptyTypeSet.
var errorEmptyTypeSet = errors.New("empty type set")

// maximumTermCount is the most terms a union's term set may hold before the computation gives up, as the original's
// does.
const maximumTermCount = 100

// termSet is the normalized terms of one type, and whether their computation is done, which tells a cycle in a
// constraint's declaration from a type met again. x/tools' typeparams termSet.
type termSet struct {
	complete bool
	terms    termList
}

// normalTerms is the terms of candidate's type set: a type parameter's normalized structural terms, an interface's or
// a union's normalized terms, and any other type as its one term. It is nil with no error for a set of every type, and
// an error for a set that is invalid, too complex, or empty. x/tools' typeparams NormalTerms.
func normalTerms(candidate types.Type) ([]*types.Term, error) {
	// The type set of an alias is its target's.
	described := types.Unalias(candidate)
	if named, isNamed := described.(*types.Named); isNamed {
		described = named.Underlying()
	}
	switch constrained := described.(type) {
	case *types.TypeParam:
		return structuralTerms(constrained)
	case *types.Union:
		return computeTermSet(constrained)
	case *types.Interface:
		return computeTermSet(constrained)
	}
	// Any other type is its own one term, as written, so an alias stays an alias.
	return []*types.Term{types.NewTerm(false, candidate)}, nil
}

// structuralTerms is the normalized terms of parameter's constraint. x/tools' typeparams StructuralTerms.
func structuralTerms(parameter *types.TypeParam) ([]*types.Term, error) {
	constraint := parameter.Constraint()
	if constraint == nil {
		return nil, fmt.Errorf("%s has nil constraint", parameter)
	}
	constraintInterface, isInterface := constraint.Underlying().(*types.Interface)
	if !isInterface {
		return nil, fmt.Errorf("constraint is %T, not *types.Interface", constraint.Underlying())
	}
	return computeTermSet(constraintInterface)
}

// computeTermSet is the normalized terms of an interface or a union, nil for every type, and errorEmptyTypeSet for
// none. x/tools' typeparams computeTermSet.
func computeTermSet(constrained types.Type) ([]*types.Term, error) {
	computed, computeError := computeTermSetOf(constrained, map[types.Type]*termSet{})
	if computeError != nil {
		return nil, computeError
	}
	if computed.terms.isEmpty() {
		return nil, errorEmptyTypeSet
	}
	if computed.terms.isAll() {
		return nil, nil
	}
	terms := []*types.Term{}
	for _, member := range computed.terms {
		terms = append(terms, types.NewTerm(member.tilde, member.denoted))
	}
	return terms, nil
}

// computeTermSetOf is the term set of candidate, remembered in seen, which finds a cycle in a constraint's
// declaration. x/tools' typeparams computeTermSetInternal, less its debug tracing, which is off in the original.
//
// Mutates: seen is the memo of one computation, which computeTermSet makes and every recursive call fills, so a
// type met again is answered from it, or found to be a cycle when its computation has not finished.
func computeTermSetOf(candidate types.Type, seen map[types.Type]*termSet) (*termSet, error) {
	if candidate == nil {
		// Invariant: every type the checker records for a constraint, an embedded type or a union's term is set,
		// an invalid one as types.Typ[types.Invalid]; the original panics here too.
		panic("composite: a term set of a nil type")
	}
	if remembered, isSeen := seen[candidate]; isSeen {
		if !remembered.complete {
			return nil, fmt.Errorf("cycle detected in the declaration of %s", candidate)
		}
		return remembered, nil
	}
	// Remembered before it is computed, so meeting it again within its own computation is a cycle.
	computing := &termSet{}
	// Done however the computation ends, an error included, as the original's deferred mark is.
	defer func() {
		computing.complete = true
	}()
	seen[candidate] = computing

	switch underlying := candidate.Underlying().(type) {
	case *types.Interface:
		// An interface's term set is the intersection of its embedded types' term sets.
		computing.terms = everyType()
		for embedded := range underlying.EmbeddedTypes() {
			// Kept for fidelity, and no input reaches it: a type parameter's underlying type is its constraint's
			// interface, never a type parameter, and the checker refuses a type parameter embedded in an interface.
			if _, isParameter := embedded.Underlying().(*types.TypeParam); isParameter {
				return nil, fmt.Errorf("invalid embedded type %T", embedded)
			}
			embeddedSet, embeddedError := computeTermSetOf(embedded, seen)
			if embeddedError != nil {
				return nil, embeddedError
			}
			computing.terms = computing.terms.intersect(embeddedSet.terms)
		}
	case *types.Union:
		// A union's term set is the union of its terms' term sets.
		computing.terms = nil
		for unionTerm := range underlying.Terms() {
			var terms termList
			switch unionTerm.Type().Underlying().(type) {
			case *types.Interface:
				termSetOfInterface, interfaceError := computeTermSetOf(unionTerm.Type(), seen)
				if interfaceError != nil {
					return nil, interfaceError
				}
				terms = termSetOfInterface.terms
			// Kept for fidelity, and no input reaches it: the checker refuses a type parameter alone as a union's
			// term, and a type parameter's or a union's underlying type is never one of these.
			case *types.TypeParam, *types.Union:
				return nil, fmt.Errorf("invalid union term %T", unionTerm)
			default:
				if unionTerm.Type() == types.Typ[types.Invalid] {
					continue
				}
				terms = termList{{tilde: unionTerm.Tilde(), denoted: unionTerm.Type()}}
			}
			computing.terms = computing.terms.union(terms)
			if len(computing.terms) > maximumTermCount {
				return nil, fmt.Errorf("exceeded max term count %d", maximumTermCount)
			}
		}
	case *types.TypeParam:
		// Invariant: a type's underlying type is never a type parameter; the original panics here too.
		panic("composite: a type parameter as an underlying type")
	default:
		// Any other type's term set is the one term of the type itself, and an invalid type's is empty.
		if underlying != types.Typ[types.Invalid] {
			computing.terms = termList{{tilde: false, denoted: candidate}}
		}
	}
	return computing, nil
}

// dereference is the type candidate points to when its core type is a pointer, and candidate otherwise. x/tools'
// typeparams Deref.
func dereference(candidate types.Type) types.Type {
	if pointer, isPointer := pointerCoreType(candidate); isPointer {
		return pointer.Elem()
	}
	return candidate
}

// pointerCoreType is candidate's core type when that is a pointer: its underlying type, or for an interface, the one
// underlying type of every type in its type set. x/tools' typeparams CoreType, less its case of a set of channels of
// mixed direction, whose core type is a channel and never a pointer, so it answers nothing dereference asks.
func pointerCoreType(candidate types.Type) (*types.Pointer, bool) {
	underlying := candidate.Underlying()
	if _, isInterface := underlying.(*types.Interface); !isInterface {
		pointer, isPointer := underlying.(*types.Pointer)
		return pointer, isPointer
	}
	// Kept for fidelity, and run never reaches it: the type it dereferences is the first of a normalized term set,
	// which expands every interface into its terms, so its underlying type is never an interface.
	terms, termsError := normalTerms(underlying)
	if len(terms) == 0 || termsError != nil {
		return nil, false
	}
	shared := terms[0].Type().Underlying()
	for _, other := range terms[1:] {
		if !types.Identical(shared, other.Type().Underlying()) {
			return nil, false
		}
	}
	pointer, isPointer := shared.(*types.Pointer)
	return pointer, isPointer
}
