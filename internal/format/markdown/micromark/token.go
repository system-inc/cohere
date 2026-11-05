package micromark

// Token is a span of the source with a type, plus the private fields constructs hang on it.
//
// One struct carries every field any construct uses, because upstream's tokens are open objects and the
// fields are read across files. The ones prefixed with an underscore upstream are unexported here.
type Token struct {
	Type  string
	Start Point
	End   Point

	// ContentType marks a chunk whose contents another tokenizer parses: flow, content, string or text.
	ContentType string
	Previous    *Token
	Next        *Token

	tokenizer                    *TokenizeContext
	container                    bool
	isInFirstContentOfListItem   bool
	contentTypeTextTrailing      bool
	open                         bool
	close                        bool
	inactive                     bool
	balanced                     bool
	gfmAutolinkLiteralWalkedInto bool

	// Spread is upstream's _spread, read and written by mdast-util-from-markdown's prepareList.
	Spread bool
	// Align is upstream's _align on a GFM table token: "left", "right", "center" or "none" per column.
	Align []string
}

// ended reports whether the token has been exited. Upstream tests `token.end` for undefined.
func (token *Token) ended() bool { return token.End.Line != 0 }
