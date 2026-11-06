package values

import (
	"math"

	"github.com/system-inc/cohere/internal/format/estree"
)

// token is upstream's token array: [type, value, startLine, startColumn, endLine, endColumn, index].
type token struct {
	kind        string
	value       []uint16
	startLine   int
	startColumn int
	endLine     int
	endColumn   int
	// endColumnIsNaN is an end column computed from an undefined `next`.
	endColumnIsNaN bool
	index          int
}

// endColumnValue is token[5] as JavaScript holds it: a number, or NaN.
func (token *token) endColumnValue() estree.Value {
	if token.endColumnIsNaN {
		return estree.Number(math.NaN())
	}
	return estree.Integer(token.endColumn)
}
