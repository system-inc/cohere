package high_level_intermediate_representation

// ManualMemoKind is which of the two memoization hooks a call site used.
//
// The two differ in what they memoize -- `useMemo` a value, `useCallback` a function -- and that
// difference decides both the rewrite and which place `FinishMemoize` records, so the distinction
// is carried rather than collapsed to a boolean.
type ManualMemoKind uint8

// String renders the kind as its source spelling.
func (kind ManualMemoKind) String() string {
	switch kind {
	case ManualMemoKindUseMemo:
		return "useMemo"
	case ManualMemoKindUseCallback:
		return "useCallback"
	}
	return "none"
}
