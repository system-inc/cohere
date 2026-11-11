package verbs

import "go/types"

// maximumTermCount is how many terms a union may normalize to before its type set is not computed, as x/tools'
// typeparams bounds it.
const maximumTermCount = 100

// termSet is a type's set of terms while termSets computes it, complete once it has.
type termSet struct {
	complete bool
	terms    termList
}

// structuralTerms are the normalized terms of typeParameter's constraint, nil for a constraint that restricts no
// type, and false when they cannot be computed: an invalid constraint, one past the bound on terms, or one whose type
// set is empty. It translates x/tools' typeparams.StructuralTerms, whose errors printf reads only as not computed.
func structuralTerms(typeParameter *types.TypeParam) ([]*typeTerm, bool) {
	constraint := typeParameter.Constraint()
	if constraint == nil {
		return nil, false
	}
	constraintInterface, isInterface := constraint.Underlying().(*types.Interface)
	if !isInterface {
		return nil, false
	}
	set, computed := termSets{}.compute(constraintInterface)
	if !computed || set.terms.isEmpty() {
		return nil, false
	}
	if set.terms.isAll() {
		return nil, true
	}
	return set.terms, true
}

// termSets are the sets one computation has computed or is computing, by type, so a declaration that cycles back
// to itself is not computed.
type termSets map[types.Type]*termSet

// compute is the term set of goType: x/tools' typeparams computeTermSetInternal.
func (seen termSets) compute(goType types.Type) (*termSet, bool) {
	if known, isSeen := seen[goType]; isSeen {
		if !known.complete {
			return nil, false // A cycle in the declaration of goType.
		}
		return known, true
	}
	set := new(termSet)
	defer func() {
		set.complete = true
	}()
	seen[goType] = set
	switch underlying := goType.Underlying().(type) {
	case *types.Interface:
		set.terms = allTerms
		for embedded := range underlying.EmbeddedTypes() {
			if _, isTypeParameter := embedded.Underlying().(*types.TypeParam); isTypeParameter {
				return nil, false
			}
			embeddedSet, computed := seen.compute(embedded)
			if !computed {
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
				interfaceSet, computed := seen.compute(unionTerm.Type())
				if !computed {
					return nil, false
				}
				terms = interfaceSet.terms
			case *types.TypeParam, *types.Union:
				return nil, false // An invalid union term.
			default:
				if unionTerm.Type() == types.Typ[types.Invalid] {
					continue
				}
				terms = termList{{tilde: unionTerm.Tilde(), termType: unionTerm.Type()}}
			}
			set.terms = set.terms.union(terms)
			if len(set.terms) > maximumTermCount {
				return nil, false
			}
		}
	case *types.TypeParam:
		// Invariant: a type parameter's underlying type is its constraint's interface, never a type parameter.
		panic("a type parameter's underlying type is a type parameter")
	default:
		if underlying != types.Typ[types.Invalid] {
			set.terms = termList{{tilde: false, termType: goType}}
		}
	}
	return set, true
}
