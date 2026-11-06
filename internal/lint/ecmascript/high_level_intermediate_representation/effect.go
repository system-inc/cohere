package high_level_intermediate_representation

// Effect is what a reference does to the value it names.
//
// Nothing computes these yet; lowering leaves every place at EffectUnknown.
//
// # This ordering is not the lattice an effect inference joins over, and reading it that way is a
// # trap that was walked into once
//
// The declaration order below runs least to most constraining, which reads like the specification
// for a meet operation. It is not one, and building a dataflow pass whose join is "take the larger
// Effect" would produce a pass that is self-consistent, passes its own tests, and is wrong.
//
// Upstream's effect inference (`InferMutationAliasingEffects`, 3,297 lines) joins over a different
// and disjoint lattice, `ValueKind`: `Mutable | Frozen | Primitive | MaybeFrozen | Global |
// Context`. Its join, `mergeValueKinds`, is not a max over an order, because it is not a total
// order at all: `Frozen` joined with `Mutable` is `MaybeFrozen`, a third element that is neither
// operand. No ordering of the constants below can express that, so a meet defined over this type
// cannot be made correct by reordering it.
//
// `Effect` is an output of that pass, written per reference by `applySignature` once the abstract
// interpretation has settled what kind of value each place holds. The abstract state is keyed by
// value, `Effect` is recorded per reference, and the two are different things: a value has one
// kind at a program point, while the several references to it at that point can read, mutate, and
// freeze it independently. That is what the `Place` comment means by the effect differing per
// reference.
//
// So a pass filling these in computes a `ValueKind` lattice it must add, and writes `Effect` as a
// result. See `Reactive` below for the other half, and the package comment for what is not here.
type Effect uint8

// IsMutable reports whether this effect can modify the value.
func (effect Effect) IsMutable() bool {
	switch effect {
	case EffectCapture, EffectStore, EffectConditionallyMutate, EffectConditionallyMutateIterator, EffectMutate:
		return true
	default:
		return false
	}
}

func (effect Effect) String() string {
	switch effect {
	case EffectFreeze:
		return "freeze"
	case EffectRead:
		return "read"
	case EffectCapture:
		return "capture"
	case EffectConditionallyMutateIterator:
		return "mutate-iterator?"
	case EffectConditionallyMutate:
		return "mutate?"
	case EffectMutate:
		return "mutate"
	case EffectStore:
		return "store"
	default:
		return "<unknown>"
	}
}
