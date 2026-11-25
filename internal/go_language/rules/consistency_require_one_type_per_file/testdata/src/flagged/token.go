package flagged

// A method that answers something is real even when it only tags its type, so Comma's kind, with a result, makes it a
// second type with methods beside Token.
type Token struct {
	text string
}

func (token Token) Text() string {
	return token.text
}

type Comma struct{} // want "already serves `Token`, and `Comma` is a second type with methods"

func (Comma) kind() int {
	return 0
}
