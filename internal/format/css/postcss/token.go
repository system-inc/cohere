package postcss

// token is postcss's token array [type, value, start, end]. start and end are unit offsets, end being
// the offset of the token's last unit; a space token has neither and a control character has no end,
// which is -1 here where upstream's array is shorter.
//
// inline is the fifth element postcss-scss's tokenizer gives a `//` comment, token[4] === 'inline'
// (scss_tokenizer.go); postcss's own tokenizer never sets it.
type token struct {
	kind   string
	value  string
	start  int
	end    int
	inline bool
}

// endOrStart is `token[3] || token[2]`: 0 is falsy there too, so a position at offset 0 falls through,
// and the result is -1 when neither is set.
func (each token) endOrStart() int {
	if each.end > 0 {
		return each.end
	}
	return each.start
}
