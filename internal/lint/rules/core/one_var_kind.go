package core

// oneVarKind names the five declaration spellings upstream tracks separately.
type oneVarKind int

// text is the spelling the message interpolates, which is upstream's `node.kind`.
func (kind oneVarKind) text() string {
	switch kind {
	case oneVarKindLet:
		return "let"
	case oneVarKindConst:
		return "const"
	case oneVarKindUsing:
		return "using"
	case oneVarKindAwaitUsing:
		return "await using"
	}
	return "var"
}
