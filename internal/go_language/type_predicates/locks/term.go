package locks

import "go/types"

// term is one elementary set of types. x/tools' typeparams.term, generated there from the type checker's own:
//
//	(*term)(nil)                       no type, the empty set
//	&term{}                            every type
//	&term{tilde: false, denoted: T}    T alone
//	&term{tilde: true, denoted: T}     every type whose underlying type is T
type term struct {
	// tilde is read only when denoted is set.
	tilde   bool
	denoted types.Type
}

// union is left and right together: no term, one, or two when they share no type.
func (left *term) union(right *term) (*term, *term) {
	switch {
	case left == nil && right == nil:
		return nil, nil
	case left == nil:
		return right, nil
	case right == nil:
		return left, nil
	case left.denoted == nil:
		return left, nil
	case right.denoted == nil:
		return right, nil
	}
	if left.disjoint(right) {
		return left, right
	}
	// The two denote the same type, so the union is the wider of them: one with a tilde holds the one without.
	if left.tilde || !right.tilde {
		return left, nil
	}
	return right, nil
}

// intersect is the types left and right share, nil when they share none.
func (left *term) intersect(right *term) *term {
	switch {
	case left == nil || right == nil:
		return nil
	case left.denoted == nil:
		return right
	case right.denoted == nil:
		return left
	}
	if left.disjoint(right) {
		return nil
	}
	// The two denote the same type, so the intersection is the narrower of them: one without a tilde.
	if !left.tilde || right.tilde {
		return left
	}
	return right
}

// disjoint reports whether left and right share no type. Both must denote a type.
func (left *term) disjoint(right *term) bool {
	leftType := left.denoted
	if right.tilde {
		leftType = leftType.Underlying()
	}
	rightType := right.denoted
	if left.tilde {
		rightType = rightType.Underlying()
	}
	return !types.Identical(leftType, rightType)
}
