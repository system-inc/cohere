package high_level_intermediate_representation

// PreserveManualMemoizationKind is which of the two conditions fired.
type PreserveManualMemoizationKind uint8

func (kind PreserveManualMemoizationKind) String() string {
	switch kind {
	case PreserveManualMemoizationDependencyMutable:
		return "this dependency may be modified later"
	case PreserveManualMemoizationValueUnmemoized:
		return "could not preserve existing memoization"
	default:
		return "<unknown preserve-manual-memoization kind>"
	}
}
