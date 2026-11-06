package mutation_aliasing

// AliasingEffectKind is what one effect does. These are React's `AliasingEffect` variants.
//
// This is the real output type of the effect inference and it is not `Effect`. See cohere's
// effects.go: the two are different types, this one carries from and into places, and the scalar
// `Effect` is a per-operand projection of a whole list of these, computed later from the ranges.
//
// The declaration order carries no meaning and there is no join over it. Unlike `ValueKind`, which
// `immutability.go` merges with a real lattice operation, these are not merged at all: an
// instruction produces a list of effects and they are all applied in order. A pass that tried to
// reduce the list to one element would lose the from/into pairing that is the entire content.
type AliasingEffectKind uint8

func (kind AliasingEffectKind) String() string {
	switch kind {
	case AliasingEffectCreate:
		return "create"
	case AliasingEffectCreateFrom:
		return "create-from"
	case AliasingEffectAssign:
		return "assign"
	case AliasingEffectAlias:
		return "alias"
	case AliasingEffectMaybeAlias:
		return "maybe-alias"
	case AliasingEffectCapture:
		return "capture"
	case AliasingEffectImmutableCapture:
		return "immutable-capture"
	case AliasingEffectMutate:
		return "mutate"
	case AliasingEffectMutateConditionally:
		return "mutate-conditionally"
	case AliasingEffectMutateTransitive:
		return "mutate-transitive"
	case AliasingEffectMutateTransitiveConditionally:
		return "mutate-transitive-conditionally"
	case AliasingEffectFreeze:
		return "freeze"
	case AliasingEffectApply:
		return "apply"
	default:
		return "<unknown>"
	}
}

// IsMutation reports whether this effect widens a value's mutable range.
//
// This is the exact predicate the ranges pass reads to build its `mutations` list, at
// `infer_mutation_aliasing_ranges.rs` where `mutations` is populated from effects matching
// `Mutate | MutateConditionally | MutateTransitive | MutateTransitiveConditionally`. Exposed here
// rather than restated there so the two passes cannot drift.
func (kind AliasingEffectKind) IsMutation() bool {
	switch kind {
	case AliasingEffectMutate, AliasingEffectMutateConditionally,
		AliasingEffectMutateTransitive, AliasingEffectMutateTransitiveConditionally:
		return true
	default:
		return false
	}
}

// IsAliasing reports whether this effect creates a from/into data-flow edge.
//
// These are the five variants the ranges pass treats identically when projecting to a scalar
// `Effect`: all five give From either Capture or Read depending on whether Into is still mutable,
// and give Into Store. See cohere's `ProjectEffects`.
func (kind AliasingEffectKind) IsAliasing() bool {
	switch kind {
	case AliasingEffectAssign, AliasingEffectAlias, AliasingEffectCapture,
		AliasingEffectCreateFrom, AliasingEffectMaybeAlias:
		return true
	default:
		return false
	}
}
