package shift

import (
	"errors"
	"fmt"
	"go/types"
)

// The type set of a type parameter, computed as x/tools' internal/typeparams computes it: normalize.go here, and
// termlist.go and typeterm.go, which x/tools generates from go/types' own, in term_list.go and term.go. Only what
// StructuralTerms reaches is translated: the printing, equality, inclusion and subset operations it never calls are
// left out.

// maximumTermCount is the most terms a union may normalize to before its type set is refused as too complex, the
// original's bound.
const maximumTermCount = 100

// errorEmptyTypeSet is a type set that holds no type at all. x/tools' typeparams.ErrEmptyTypeSet.
var errorEmptyTypeSet = errors.New("empty type set")

// structuralTerms is the normalized union of tilde terms that restricts typeParameter's type set, nil when nothing
// restricts it, and an error when its constraint is invalid, too complex, or holds no type. The terms come in a
// deterministic order, which the shift check does not read. x/tools' typeparams.StructuralTerms.
func structuralTerms(typeParameter *types.TypeParam) ([]*types.Term, error) {
	constraint := typeParameter.Constraint()
	if constraint == nil {
		return nil, fmt.Errorf("%s has nil constraint", typeParameter)
	}
	constraintInterface, isInterface := constraint.Underlying().(*types.Interface)
	if !isInterface {
		return nil, fmt.Errorf("constraint is %T, not *types.Interface", constraint.Underlying())
	}
	computed, computeError := termSetOf(constraintInterface, map[types.Type]*termSet{})
	if computeError != nil {
		return nil, computeError
	}
	if computed.terms.isEmpty() {
		return nil, errorEmptyTypeSet
	}
	if computed.terms.isAll() {
		return nil, nil
	}
	var terms []*types.Term
	for _, normalized := range computed.terms {
		terms = append(terms, types.NewTerm(normalized.tilde, normalized.termType))
	}
	return terms, nil
}

// termSet is the normalized terms of one type, and whether computing them has finished, so a type met again before
// it has is a cycle. x/tools' typeparams.termSet.
type termSet struct {
	complete bool
	terms    termList
}

// termSetOf is the term set of typeOf: the intersection of its embedded types' for an interface, the union of its
// terms' for a union, and the type alone for any other. seen holds every type begun, so each is computed once and a
// cycle is refused. x/tools' typeparams.computeTermSetInternal, less its debugging output.
//
// Mutates: seen the memo of every type begun, shared down the recursion so a cycle is met as a type not yet complete
func termSetOf(typeOf types.Type, seen map[types.Type]*termSet) (*termSet, error) {
	if typeOf == nil {
		// Invariant: every type handed here is a constraint, an embedded type or a union's term, none of them nil.
		panic("shift: nil type")
	}
	if begun, isSeen := seen[typeOf]; isSeen {
		if !begun.complete {
			return nil, fmt.Errorf("cycle detected in the declaration of %s", typeOf)
		}
		return begun, nil
	}
	computing := new(termSet)
	// Complete on every return, an error's too, as the original's deferred marking is.
	defer func() {
		computing.complete = true
	}()
	seen[typeOf] = computing

	switch underlying := typeOf.Underlying().(type) {
	case *types.Interface:
		computing.terms = allTerms
		for embedded := range underlying.EmbeddedTypes() {
			if _, isTypeParameter := embedded.Underlying().(*types.TypeParam); isTypeParameter {
				return nil, fmt.Errorf("invalid embedded type %T", embedded)
			}
			embeddedSet, embeddedError := termSetOf(embedded, seen)
			if embeddedError != nil {
				return nil, embeddedError
			}
			computing.terms = computing.terms.intersect(embeddedSet.terms)
		}
	case *types.Union:
		computing.terms = nil
		for unionTerm := range underlying.Terms() {
			var terms termList
			switch unionTerm.Type().Underlying().(type) {
			case *types.Interface:
				interfaceSet, interfaceError := termSetOf(unionTerm.Type(), seen)
				if interfaceError != nil {
					return nil, interfaceError
				}
				terms = interfaceSet.terms
			case *types.TypeParam, *types.Union:
				// A type parameter or a union standing alone is not permitted as a union's term.
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
		// Invariant: a type parameter's underlying type is its constraint's interface, never a type parameter.
		panic("shift: a type parameter underlies a type")
	default:
		if underlying != types.Typ[types.Invalid] {
			computing.terms = termList{{tilde: false, termType: typeOf}}
		}
	}
	return computing, nil
}
