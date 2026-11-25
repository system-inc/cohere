package verbs

import "go/types"

// typeTerm is one elementary type set, as x/tools' typeparams term is: a nil term is the empty set, a term with no
// type the set of every type, a term without tilde the set of its one type, and a term with tilde the set of every
// type whose underlying type is its type.
type typeTerm struct {
	// tilde is read only when termType is set.
	tilde    bool
	termType types.Type
}

// union is term ∪ other, as none, one or two terms: x/tools' typeparams term.union.
func (term *typeTerm) union(other *typeTerm) (*typeTerm, *typeTerm) {
	switch {
	case term == nil && other == nil:
		return nil, nil // ∅ ∪ ∅ == ∅
	case term == nil:
		return other, nil // ∅ ∪ other == other
	case other == nil:
		return term, nil // term ∪ ∅ == term
	case term.termType == nil:
		return term, nil // 𝓤 ∪ other == 𝓤
	case other.termType == nil:
		return other, nil // term ∪ 𝓤 == 𝓤
	}
	if term.disjoint(other) {
		return term, other // term ∪ other == (term, other) when term ∩ other == ∅
	}
	// The two have one type: ~t ∪ ~t == ~t, ~t ∪ T == ~t, T ∪ ~t == ~t and T ∪ T == T.
	if term.tilde || !other.tilde {
		return term, nil
	}
	return other, nil
}

// intersect is term ∩ other: x/tools' typeparams term.intersect.
func (term *typeTerm) intersect(other *typeTerm) *typeTerm {
	switch {
	case term == nil || other == nil:
		return nil // ∅ ∩ other == ∅ and term ∩ ∅ == ∅
	case term.termType == nil:
		return other // 𝓤 ∩ other == other
	case other.termType == nil:
		return term // term ∩ 𝓤 == term
	}
	if term.disjoint(other) {
		return nil
	}
	// The two have one type: ~t ∩ ~t == ~t, ~t ∩ T == T, T ∩ ~t == T and T ∩ T == T.
	if !term.tilde || other.tilde {
		return term
	}
	return other
}

// disjoint reports whether term ∩ other == ∅, for two terms that both have a type: x/tools' typeparams
// term.disjoint.
func (term *typeTerm) disjoint(other *typeTerm) bool {
	termType := term.termType
	if other.tilde {
		termType = termType.Underlying()
	}
	otherType := other.termType
	if term.tilde {
		otherType = otherType.Underlying()
	}
	return !types.Identical(termType, otherType)
}
