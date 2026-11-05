package high_level_intermediate_representation

// FunctionKind is what a function syntactically looked like.
type FunctionKind uint8

func (kind FunctionKind) String() string {
	switch kind {
	case FunctionKindComponent:
		return "component"
	case FunctionKindHook:
		return "hook"
	default:
		return "other"
	}
}
