package iface_assert

import (
	"errors"
	"fmt"
	"go/types"
)

// maximumTermCount is the most terms a union's normal form may hold before the term set is given up. go/types refuses a
// union whose normal form holds more, so in a package that type-checks the limit is never passed.
const maximumTermCount = 100

// errorEmptyTypeSet is the error of an interface whose term set holds no type, which x/tools' typeparams calls
// ErrEmptyTypeSet. The pass reads only that there is an error, never which.
var errorEmptyTypeSet = errors.New("empty type set")

// termSet is the normalized set of terms of one type while its term set is computed: what its non-interface types
// restrict it to, which is not its type set, since its methods are left out. x/tools' typeparams.termSet.
type termSet struct {
	// complete is whether the computation of terms has returned, so a type met again before it has is a cycle.
	complete bool
	terms    termList
}

// interfaceTermSet is the normalized terms of interfaceType, nil when they hold every type, and an error when they
// cannot be computed or hold no type. x/tools' typeparams.InterfaceTermSet, which hands each term back as a
// *types.Term; the pass reads only each term's type, so these are the terms themselves.
func interfaceTermSet(interfaceType *types.Interface) (termList, error) {
	computed, computeError := computeTermSet(interfaceType, map[types.Type]*termSet{})
	if computeError != nil {
		return nil, computeError
	}
	// Kept for fidelity, though the pass cannot tell it apart: an empty set's error and an empty list both leave the
	// search finding nothing among the terms, so no mutant of this check can go red.
	if computed.terms.isEmpty() {
		return nil, errorEmptyTypeSet
	}
	if computed.terms.isAll() {
		return nil, nil
	}
	return computed.terms, nil
}

// computeTermSet is the term set of computed, each type's set kept in seen as it is begun, so a type met again before
// its set is complete is a cycle. x/tools' typeparams.computeTermSetInternal, less its trace, which only x/tools'
// debug builds print.
//
// Mutates: seen is the computation's memory, one map that interfaceTermSet makes and every recursion shares, so each
// set begun is there for the types after it.
func computeTermSet(computed types.Type, seen map[types.Type]*termSet) (*termSet, error) {
	if computed == nil {
		// Invariant: every caller hands over a type it holds: the interface the pass asks about, a type it embeds,
		// or a term of its union, none of which go/types leaves nil.
		panic("iface_assert: the term set of a nil type")
	}
	if begun, isSeen := seen[computed]; isSeen {
		// Kept for fidelity, and no input reaches it: go/types refuses an interface that embeds itself, directly or
		// through others.
		if !begun.complete {
			return nil, fmt.Errorf("cycle detected in the declaration of %s", computed)
		}
		return begun, nil
	}
	computing := new(termSet)
	// The set is complete once this returns, however it returns, as x/tools' deferred assignment marks it.
	defer func() {
		computing.complete = true
	}()
	seen[computed] = computing

	switch underlying := computed.Underlying().(type) {
	case *types.Interface:
		// An interface's term set is the intersection of the term sets of the types it embeds.
		computing.terms = allTerms
		for embedded := range underlying.EmbeddedTypes() {
			// Kept for fidelity, and no input reaches it: a type parameter's Underlying is its constraint's
			// interface, never the type parameter, and go/types refuses an interface that embeds one.
			if _, isTypeParameter := embedded.Underlying().(*types.TypeParam); isTypeParameter {
				return nil, fmt.Errorf("invalid embedded type %T", embedded)
			}
			embeddedSet, embeddedError := computeTermSet(embedded, seen)
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
				interfaceSet, interfaceError := computeTermSet(unionTerm.Type(), seen)
				if interfaceError != nil {
					return nil, interfaceError
				}
				terms = interfaceSet.terms
			case *types.TypeParam, *types.Union:
				// Kept for fidelity, and no input reaches it: go/types refuses a type parameter alone as a union's
				// term, and flattens a union inside a union into it.
				return nil, fmt.Errorf("invalid union term %T", unionTerm)
			default:
				if unionTerm.Type() == types.Typ[types.Invalid] {
					continue
				}
				terms = termList{{tilde: unionTerm.Tilde(), termType: unionTerm.Type()}}
			}
			computing.terms = computing.terms.union(terms)
			if len(computing.terms) > maximumTermCount {
				return nil, fmt.Errorf("exceeded max term count %d", maximumTermCount)
			}
		}
	case *types.TypeParam:
		// Invariant: a type parameter's Underlying is its constraint's interface, so no type's Underlying is a type
		// parameter.
		panic("iface_assert: a type whose underlying type is a type parameter")
	default:
		// Any other type's term set is the one term that holds the type itself, without a tilde.
		if underlying != types.Typ[types.Invalid] {
			computing.terms = termList{{tilde: false, termType: computed}}
		}
	}
	return computing, nil
}
