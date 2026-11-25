package high_level_intermediate_representation

// InstructionKind is the binding form a store establishes.
//
// `Const` and `Let` come from the declaration; `Reassign` is a store to an existing binding.
// Upstream reclassifies between these after single-assignment form, which is why the distinction is
// carried rather than collapsed into "store".
type InstructionKind uint8

func (kind InstructionKind) String() string {
	switch kind {
	case InstructionKindLet:
		return "let"
	case InstructionKindReassign:
		return "reassign"
	case InstructionKindCatch:
		return "catch"
	case InstructionKindHoistedConst:
		return "hoisted const"
	case InstructionKindHoistedLet:
		return "hoisted let"
	case InstructionKindHoistedFunction:
		return "hoisted function"
	case InstructionKindFunction:
		return "function"
	default:
		return "const"
	}
}
