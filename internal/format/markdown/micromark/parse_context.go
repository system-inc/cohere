package micromark

// ParseContext is the parser: the combined constructs and the tokenizer factories.
type ParseContext struct {
	Constructs *FullConstructs
	Defined    []string
	Lazy       map[int]bool

	// GfmFootnotes is upstream's parser.gfmFootnotes, which micromark-extension-gfm-footnote hangs on the
	// parser: the identifiers of the footnote definitions seen so far.
	GfmFootnotes []string

	// memory is where the parse's tokens, attempts and tokenizers come from, nil to allocate each (#93dpede,
	// #vbjv3d6).
	memory *Memory
}

// create makes a tokenizer for a content type, upstream's parser.document() and friends.
func (parser *ParseContext) create(contentType string, from *Point) *TokenizeContext {
	var initial *InitialConstruct
	switch contentType {
	case ContentTypeDocument:
		initial = documentInitial
	case ContentTypeFlow:
		initial = flowInitial
	case ContentTypeContent:
		initial = contentInitial
	case ContentTypeString:
		initial = stringInitial
	case ContentTypeText:
		initial = textInitial
	default:
		panic("micromark: unknown content type " + contentType)
	}
	return createTokenizer(parser, initial, from)
}
