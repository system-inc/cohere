package tokens

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// List is a node's tokens in source order.
type List []Token

// Before is ESLint's getTokenBefore: the last token ending at or before position.
func (list List) Before(position int) (Token, bool) {
	for index := len(list) - 1; index >= 0; index-- {
		if list[index].End <= position {
			return list[index], true
		}
	}
	return Token{}, false
}

// After is ESLint's getTokenAfter: the first token starting at or after position.
func (list List) After(position int) (Token, bool) {
	for _, token := range list {
		if token.Start >= position {
			return token, true
		}
	}
	return Token{}, false
}

// FirstBetween is ESLint's getFirstTokenBetween for one kind: the first token of that kind lying wholly
// within [from, to).
func (list List) FirstBetween(from int, to int, kind ast.Kind) (Token, bool) {
	for _, token := range list {
		if token.Start >= from && token.End <= to && token.Kind == kind {
			return token, true
		}
	}
	return Token{}, false
}
