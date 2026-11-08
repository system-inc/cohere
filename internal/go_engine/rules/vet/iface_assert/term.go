package iface_assert

import "go/types"

// term is one elementary type set, as x/tools' typeparams.term is, which copies go/types' own:
//
//	the empty set:      (*term)(nil)
//	every type:         &term{}
//	the type T alone:   &term{tilde: false, termType: T}
//	~t:                 &term{tilde: true, termType: t}, every type whose underlying type is t
type term struct {
	// tilde is whether the term holds every type whose underlying type is termType, and is read only when termType
	// is set.
	tilde bool
	// termType is the term's type, and nil for the term that holds every type.
	termType types.Type
}

// union is left ∪ right as one or two non-nil terms, the second nil when the union is one term. x/tools'
// typeparams.term.union.
func (left *term) union(right *term) (*term, *term) {
	switch {
	case left == nil && right == nil:
		return nil, nil
	case left == nil:
		return right, nil
	case right == nil:
		return left, nil
	case left.termType == nil:
		return left, nil
	case right.termType == nil:
		return right, nil
	}
	if left.disjoint(right) {
		return left, right
	}
	// The two hold the same type, so the one with a tilde, which holds the other, is the union: ~t ∪ ~t, ~t ∪ T and
	// T ∪ ~t are ~t, and T ∪ T is T.
	if left.tilde || !right.tilde {
		return left, nil
	}
	return right, nil
}

// intersect is left ∩ right, nil when it is empty. x/tools' typeparams.term.intersect.
func (left *term) intersect(right *term) *term {
	switch {
	case left == nil || right == nil:
		return nil
	case left.termType == nil:
		return right
	case right.termType == nil:
		return left
	}
	if left.disjoint(right) {
		return nil
	}
	// The two hold the same type, so the one without a tilde, which the other holds, is the intersection: ~t ∩ T and
	// T ∩ ~t are T, and ~t ∩ ~t is ~t.
	if !left.tilde || right.tilde {
		return left
	}
	return right
}

// disjoint is whether left ∩ right is empty, for two terms that both hold a type. x/tools' typeparams.term.disjoint,
// less its check of that precondition, which runs only in x/tools' debug builds.
func (left *term) disjoint(right *term) bool {
	// A tilde on one side compares the other side's underlying type, since ~t holds T exactly when T's underlying
	// type is t.
	leftCompared := left.termType
	if right.tilde {
		leftCompared = leftCompared.Underlying()
	}
	rightCompared := right.termType
	if left.tilde {
		rightCompared = rightCompared.Underlying()
	}
	return !types.Identical(leftCompared, rightCompared)
}
