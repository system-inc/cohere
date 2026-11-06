package cst

// Token is any CST token, upstream's Token union. See the package comment for which fields each type has.
type Token struct {
	Type   string
	Offset int
	Indent int
	Source []uint16

	// Message is an error token's message.
	Message string

	// Start is a document's start: the tokens before its value.
	Start []*Token
	// FlowStart is a flow collection's start, its { or [ token.
	FlowStart *Token
	// Value is a document's value, nil when absent.
	Value *Token
	// End is the tokens after a document, document end marker, flow scalar or flow collection; nil when
	// absent.
	End []*Token
	// Properties is a block scalar's header, and the comments and spaces on its line.
	Properties []*Token
	// Items is a block map's, block sequence's or flow collection's items.
	Items []*CollectionItem

	// indentAbsent marks a token whose upstream object has no indent field: a document, directive,
	// document end or error token the parser builds. SourceTokens always have one, whatever their type.
	indentAbsent bool
}

// HasIndent is upstream's `'indent' in token`.
func (token *Token) HasIndent() bool { return !token.indentAbsent }

// HasSource is upstream's `'source' in token`: every token but a document and a collection has one.
func (token *Token) HasSource() bool {
	switch token.Type {
	case "document", "block-map", "block-seq", "flow-collection":
		return false
	}
	return true
}
