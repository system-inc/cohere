package core

// oneDeclarationKind names the five declaration spellings upstream tracks separately.
type oneDeclarationKind int

// text is the spelling the message interpolates, which is upstream's `node.kind`.
func (kind oneDeclarationKind) text() string {
	switch kind {
	case oneDeclarationKindLet:
		return "let"
	case oneDeclarationKindConst:
		return "const"
	case oneDeclarationKindUsing:
		return "using"
	case oneDeclarationKindAwaitUsing:
		return "await using"
	}
	return "var"
}
