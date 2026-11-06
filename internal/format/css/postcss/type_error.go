package postcss

// TypeError is the JavaScript TypeError postcss-scss throws where its rule reads tokens[0] of an empty
// array (a nested property with no word in it, `:"x" {}`). Prettier only turns errors with a line into its
// own and lets this one escape, so the file does not format; ParseSCSS marks it with printing.Syntax.
type TypeError struct {
	// Message is the error's message as V8 words it.
	Message string
}

func (typeError *TypeError) Error() string {
	return "TypeError: " + typeError.Message
}
