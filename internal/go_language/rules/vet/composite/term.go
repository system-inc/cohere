package composite

import "go/types"

// term is one elementary type set, as a term of a constraint's union spells it. x/tools' typeparams term, itself the
// compiler's types2 typeterm.go:
//
//	nil:                        the empty set
//	&term{}:                    every type
//	&term{false, T}:            the type T alone
//	&term{true, T}:             every type whose underlying type is T, `~T`
type term struct {
	// tilde is whether the term is `~T`, read only when denoted is set.
	tilde bool
	// denoted is the term's type, and nil for the term of every type.
	denoted types.Type
}

// union is left ∪ right, as zero, one or two terms: right is returned beside left only when the two are disjoint.
// x/tools' typeparams term.union.
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
	// The two share a type, so the union is the wider of them: `~T` over `T`, and left when they are alike.
	if left.tilde || !right.tilde {
		return left, nil
	}
	return right, nil
}

// intersect is left ∩ right, nil when it is empty. x/tools' typeparams term.intersect.
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
	// The two share a type, so the intersection is the narrower of them: `T` under `~T`, and left when they are
	// alike.
	if !left.tilde || right.tilde {
		return left
	}
	return right
}

// disjoint is whether left ∩ right is empty, for two terms that each denote a type: each side's type is compared
// with the other's, as its underlying type where the other is a tilde term. x/tools' typeparams term.disjoint.
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
