package selector

// token is upstream's token array. kind is token[0], value token[1], and rest holds token[2:], whose
// meaning depends on the kind: [line, column, pos] for a space, combinator or single character, and
// [line, column, endLine, endColumn, pos] for a string, at-word, word or comment.
type token struct {
	kind  string
	value string
	rest  []int
}

// at is token[index] for index 2 and up, the way the parser reads it.
func (token *token) at(index int) int { return token.rest[index-2] }

// loose is token[index] where the token may be shorter than the parser assumes, and whether the
// token is that long, false standing for undefined. splitWord can leave currToken on a space token (a
// word ending in an escaped space, with nothing word-like after it), and upstream then reads that
// token's [4] (its pos), [5] and [6] (undefined) as if it were a word's.
func (token *token) loose(index int) (int, bool) {
	if index-2 < len(token.rest) {
		return token.rest[index-2], true
	}
	return 0, false
}
