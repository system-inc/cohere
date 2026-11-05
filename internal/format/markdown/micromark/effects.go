package micromark

// Effects are the tools a construct tokenizes with.
type Effects struct {
	Attempt   func(constructs Constructs, returnState State, bogusState State) State
	Check     func(constructs Constructs, returnState State, bogusState State) State
	Interrupt func(constructs Constructs, returnState State, bogusState State) State
	Consume   func(code Code)
	// Enter opens a token. A non-nil token is upstream's `fields`: it becomes the token, with its type
	// and start set.
	Enter func(tokenType string, token *Token) *Token
	Exit  func(tokenType string) *Token

	// tokenizer is the tokenizer these effects belong to, through which a factory reaches the parse's
	// Memory (#vbjv3d6).
	tokenizer *TokenizeContext
}

// memory is the Memory of the parse these effects tokenize, nil when it allocates each value.
func (effects *Effects) memory() *Memory { return effects.tokenizer.Parser.memory }
