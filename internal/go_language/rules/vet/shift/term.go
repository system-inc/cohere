package shift

import "go/types"

// term is one elementary type set: nil is the empty set, a term with no type is every type, a term without a tilde
// is its type alone, and one with a tilde is every type whose underlying type is its type. x/tools' typeparams.term.
type term struct {
	// tilde is read only when termType is set.
	tilde    bool
	termType types.Type
}

// union is first's set joined with second's: none, one or two terms, the second set only when the two are disjoint.
// x/tools' term.union.
func (first *term) union(second *term) (*term, *term) {
	switch {
	case first == nil && second == nil:
		return nil, nil
	case first == nil:
		return second, nil
	case second == nil:
		return first, nil
	case first.termType == nil:
		return first, nil
	case second.termType == nil:
		return second, nil
	}
	if first.disjoint(second) {
		return first, second
	}
	// The two share a type, so the one with the tilde, if either has it, holds the other.
	if first.tilde || !second.tilde {
		return first, nil
	}
	return second, nil
}

// intersect is what first's set and second's share, nil when nothing. x/tools' term.intersect.
func (first *term) intersect(second *term) *term {
	switch {
	case first == nil || second == nil:
		return nil
	case first.termType == nil:
		return second
	case second.termType == nil:
		return first
	}
	if first.disjoint(second) {
		return nil
	}
	// The two share a type, so the one without the tilde, if either lacks it, is what they share.
	if !first.tilde || second.tilde {
		return first
	}
	return second
}

// disjoint is whether first and second share no type. Both must have a type. x/tools' term.disjoint.
func (first *term) disjoint(second *term) bool {
	firstType := first.termType
	if second.tilde {
		firstType = firstType.Underlying()
	}
	secondType := second.termType
	if first.tilde {
		secondType = secondType.Underlying()
	}
	return !types.Identical(firstType, secondType)
}
