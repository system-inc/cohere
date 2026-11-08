package printf

import (
	"errors"
	"go/ast"
	"go/token"
	"strconv"
	"unicode/utf8"
)

// sourceRange is a span of positions a finding covers, an analysis.Range: x/tools' astutil.Range.
type sourceRange struct {
	start token.Pos
	end   token.Pos
}

// Pos is where the range starts.
func (span sourceRange) Pos() token.Pos {
	return span.start
}

// End is where the range ends.
func (span sourceRange) End() token.Pos {
	return span.end
}

// rangeInStringLiteral is the span of literal's source that holds the bytes start to end of the string it denotes,
// or the error that the literal does not unquote or an offset is outside it: x/tools' astutil.RangeInStringLiteral.
func rangeInStringLiteral(literal *ast.BasicLit, start int, end int) (sourceRange, error) {
	startPosition, startError := positionInStringLiteral(literal, start)
	if startError != nil {
		return sourceRange{}, startError
	}
	endPosition, endError := positionInStringLiteral(literal, end)
	if endError != nil {
		return sourceRange{}, endError
	}
	return sourceRange{start: startPosition, end: endPosition}, nil
}

// positionInStringLiteral is the position in literal's source of the byte at offset in the string it denotes:
// x/tools' astutil.PosInStringLiteral.
func positionInStringLiteral(literal *ast.BasicLit, offset int) (token.Pos, error) {
	value, unquoteError := strconv.Unquote(literal.Value)
	if unquoteError != nil {
		return token.NoPos, unquoteError
	}
	if offset < 0 || offset > len(value) {
		return token.NoPos, errors.New("invalid offset")
	}
	position := literal.Pos() + 1 // Past the opening quote.
	// A raw string's carriage returns are in the source and not the literal's value, so its source is longer.
	normalized := int(literal.End()-literal.Pos()) > len(literal.Value)
	quote := literal.Value[0]
	raw := literal.Value[1 : len(literal.Value)-1]
	logical := 0
	for raw != "" {
		// Ignored: the literal unquoted above, so each of its characters does too.
		character, _, rest, _ := strconv.UnquoteChar(raw, quote)
		size := len(raw) - len(rest)
		nextPosition := position + token.Pos(size)
		if normalized && character == '\n' {
			nextPosition++
		}
		nextLogical := logical + utf8.RuneLen(character)
		if nextPosition > literal.End() || nextLogical > offset {
			break
		}
		raw = raw[size:]
		logical = nextLogical
		position = nextPosition
	}
	return position, nil
}
