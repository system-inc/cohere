package locks

import "go/types"

// maximumTermCount bounds the terms a union may normalize to before the computation gives up, as the original's does.
const maximumTermCount = 100

// termSet is the normalized terms of one type, met while computing a constraint's, and whether they are complete: a
// type met again before its terms are complete declares itself through a cycle. x/tools' typeparams.termSet.
type termSet struct {
	complete bool
	terms    termList
}

// structuralTerms is the normalized terms of typeParameter's constraint, the types it permits, each one exactly or by
// its underlying type: for `[P interface{ ~int; m() }]`, the one term ~int. The order is the original's, which is
// deterministic. It is nil when the constraint is invalid, too complex, has an empty type set, or restricts no type
// at all, since Of finds no lock in any of those. x/tools' typeparams.StructuralTerms, with its errors, which
// Of only tested for, folded into the nil result.
func structuralTerms(typeParameter *types.TypeParam) []*types.Term {
	constraint := typeParameter.Constraint()
	if constraint == nil {
		return nil
	}
	constraintInterface, isInterface := constraint.Underlying().(*types.Interface)
	if !isInterface {
		return nil
	}
	set, isComputed := computeTermSet(constraintInterface, map[types.Type]*termSet{})
	if !isComputed || set.terms.isEmpty() || set.terms.isAll() {
		return nil
	}
	terms := []*types.Term{}
	for _, member := range set.terms {
		terms = append(terms, types.NewTerm(member.tilde, member.denoted))
	}
	return terms
}

// computeTermSet is typed's normalized terms, with seen holding the set of each type met so far, and false when they
// cannot be computed: a cycle, an invalid embedding or union term, or too many terms. An interface's terms are the
// intersection of its embedded types', a union's are the union of its terms', and any other type's is the type
// itself. x/tools' typeparams.computeTermSetInternal, less its debugging output.
//
// Mutates: seen recording typed's set as met, since the computation of every type typed holds shares the one map.
func computeTermSet(typed types.Type, seen map[types.Type]*termSet) (*termSet, bool) {
	if met, isMet := seen[typed]; isMet {
		if !met.complete {
			return nil, false
		}
		return met, true
	}
	// Marked as met before its parts are walked, so a type that embeds itself is caught as a cycle rather than
	// recursing forever, and complete however the walk ends.
	set := &termSet{}
	defer func() {
		set.complete = true
	}()
	seen[typed] = set

	switch underlying := typed.Underlying().(type) {
	case *types.Interface:
		set.terms = allTermList
		for embedded := range underlying.EmbeddedTypes() {
			// Kept from the original, though it cannot fire: a type parameter's underlying type is its constraint's
			// interface, so no embedded type has a type parameter as its underlying type.
			if _, isTypeParameter := embedded.Underlying().(*types.TypeParam); isTypeParameter {
				return nil, false
			}
			embeddedSet, isComputed := computeTermSet(embedded, seen)
			if !isComputed {
				return nil, false
			}
			set.terms = set.terms.intersect(embeddedSet.terms)
		}
	case *types.Union:
		set.terms = nil
		for unionTerm := range underlying.Terms() {
			var terms termList
			switch unionTerm.Type().Underlying().(type) {
			case *types.Interface:
				embeddedSet, isComputed := computeTermSet(unionTerm.Type(), seen)
				if !isComputed {
					return nil, false
				}
				terms = embeddedSet.terms
			case *types.TypeParam, *types.Union:
				// A lone type parameter or union may not be a union's term.
				return nil, false
			default:
				if unionTerm.Type() == types.Typ[types.Invalid] {
					continue
				}
				terms = termList{{tilde: unionTerm.Tilde(), denoted: unionTerm.Type()}}
			}
			set.terms = set.terms.union(terms)
			if len(set.terms) > maximumTermCount {
				return nil, false
			}
		}
	case *types.TypeParam:
		// No type has a type parameter as its underlying type (see above). The original panics here; the port gives
		// up on the constraint instead, which is the same where it cannot happen.
		return nil, false
	default:
		if underlying != types.Typ[types.Invalid] {
			set.terms = termList{{tilde: false, denoted: typed}}
		}
	}
	return set, true
}
