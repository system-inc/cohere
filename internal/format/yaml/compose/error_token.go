package compose

import "github.com/system-inc/cohere/internal/format/yaml/cst"

// errorToken is an error placed at a CST token.
type errorToken struct {
	token *cst.Token
}

// position is getErrorPos for a token: its source, or one character for a token without one.
func (source errorToken) position() [2]int {
	if source.token.HasSource() {
		return [2]int{source.token.Offset, source.token.Offset + len(source.token.Source)}
	}
	return [2]int{source.token.Offset, source.token.Offset + 1}
}
