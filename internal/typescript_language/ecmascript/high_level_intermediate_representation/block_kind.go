package high_level_intermediate_representation

// BlockKind is what a block was created for.
//
// It is upstream's, and it matters to codegen and to reactive scopes rather than to analysis. It is
// carried because the fixtures' expectations were produced by a compiler that had it, and dropping
// it would make a later divergence harder to explain.
type BlockKind uint8

func (kind BlockKind) String() string {
	switch kind {
	case BlockKindValue:
		return "value"
	case BlockKindLoop:
		return "loop"
	case BlockKindSequence:
		return "sequence"
	case BlockKindCatch:
		return "catch"
	default:
		return "block"
	}
}
