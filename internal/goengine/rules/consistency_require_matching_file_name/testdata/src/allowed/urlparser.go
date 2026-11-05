package allowed

// Where a word ends inside a run of capitals is a guess, so a file spelling the type's letters in any case and with
// or without underscores is named for it.
type URLParser struct {
	source string
}

func (parser URLParser) Source() string {
	return parser.source
}
