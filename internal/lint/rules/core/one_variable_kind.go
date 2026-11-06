package core

// oneVariableKind names the five declaration spellings upstream tracks separately.
type oneVariableKind int

// text is the spelling the message interpolates, which is upstream's `node.kind`.
func (kind oneVariableKind) text() string {
	switch kind {
	case oneVariableKindLet:
		return "let"
	case oneVariableKindConst:
		return "const"
	case oneVariableKindUsing:
		return "using"
	case oneVariableKindAwaitUsing:
		return "await using"
	}
	return "var"
}
