package high_level_intermediate_representation

// EffectGap names an effect-inference rule this pass cannot apply, for a caller that needs to know.
//
// Named in the API rather than only in a comment, the way `ReactiveGaps` and `RangeGaps` are, for
// the reason `reactivity.go` gives: a caller that needs the missing half should be able to ask rather
// than discover it. Both React and oxc treat a half-filled effect table as an invariant violation
// rather than a skippable default, so a consumer being able to enumerate what is missing is the
// difference between an under-approximation it can reason about and a table that silently lies.
type EffectGap uint8

func (gap EffectGap) String() string {
	switch gap {
	case EffectGapInterproceduralParameters:
		return "interprocedural-parameters"
	case EffectGapSignatureTable:
		return "signature-table"
	case EffectGapTypeDirectedShapes:
		return "type-directed-shapes"
	default:
		return "<unknown>"
	}
}
