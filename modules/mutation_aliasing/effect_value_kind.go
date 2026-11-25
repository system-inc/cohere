package mutation_aliasing

// EffectValueKind is the abstract kind a Create effect produces.
//
// This is upstream's `ValueKind` and it is deliberately not the same type as the six-element
// lattice cohere's `immutability.go` builds for itself. That rule's lattice is a private type with a
// join (`immutabilityMergeKinds`) tuned to what it reports; this kind also feeds the range graph's
// phi refinement. Sharing one type across the two would couple a rule's message selection to a
// substrate enum for no gain, and `immutability.go`'s version carries a reason bitset this one has no
// use for.
type EffectValueKind uint8

func (kind EffectValueKind) String() string {
	switch kind {
	case EffectValuePrimitive:
		return "primitive"
	case EffectValueFrozen:
		return "frozen"
	case EffectValueMaybeFrozen:
		return "maybe-frozen"
	case EffectValueGlobal:
		return "global"
	default:
		return "mutable"
	}
}
