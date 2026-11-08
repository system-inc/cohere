package printf

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// formatParser is the state of parsing one operation: x/tools' fmtstr state.
type formatParser struct {
	operation *formatOperation

	// firstArgument is the index in the call's arguments of the first argument after the format string.
	firstArgument int

	// argumentNumber is the index of the argument the operation formats next.
	argumentNumber int

	// index is the last `[n]` read, and indexPosition where its `[` sits in the operation.
	index         int
	indexPosition int

	// indexPending is set while an `[n]` has been read that no `*` or verb has taken yet.
	indexPending bool

	// consumed is how many bytes of the operation's text the parser has read.
	consumed int
}

// parseOperation parses the one operation format starts with, at its `%`, whose arguments start at firstArgument and
// which formats argumentNumber next unless it says otherwise, or is the error that it is malformed: x/tools' fmtstr
// parseOperation. The order is `%`, flags, `[n]*` or a width, `.` and `[n]*` or a precision, `[n]`, the verb.
func parseOperation(format string, firstArgument int, argumentNumber int) (*formatParser, error) {
	parser := &formatParser{
		operation: &formatOperation{
			text:      format,
			width:     emptySize,
			precision: emptySize,
		},
		firstArgument:  firstArgument,
		argumentNumber: argumentNumber,
		// The format starts with its percent sign.
		consumed: len("%"),
	}
	parser.parseFlags()
	if indexError := parser.parseIndex(); indexError != nil {
		return nil, indexError
	}
	parser.operation.width = parser.parseSize()
	if precisionError := parser.parsePrecision(); precisionError != nil {
		return nil, precisionError
	}
	// The verb, maybe after an index, unless one was read already and is waiting for it.
	if !parser.indexPending {
		if indexError := parser.parseIndex(); indexError != nil {
			return nil, indexError
		}
	}
	if parser.consumed == len(parser.operation.text) {
		return nil, fmt.Errorf("format %s is missing verb at end of string", parser.operation.text)
	}
	verb, width := utf8.DecodeRuneInString(parser.operation.text[parser.consumed:])
	if parser.indexPending {
		parser.operation.verb = formatVerb{
			verb:          verb,
			byteRange:     byteRange{start: parser.indexPosition, end: parser.consumed + width},
			index:         parser.index,
			argumentIndex: parser.argumentNumber,
		}
	} else {
		parser.operation.verb = formatVerb{
			verb:          verb,
			byteRange:     byteRange{start: parser.consumed, end: parser.consumed + width},
			index:         -1,
			argumentIndex: parser.argumentNumber,
		}
	}
	parser.consumed += width
	parser.operation.text = parser.operation.text[:parser.consumed]
	return parser, nil
}

// parseFlags reads whatever printf flags come next.
func (parser *formatParser) parseFlags() {
	remaining := parser.operation.text[parser.consumed:]
	parser.operation.flags = remaining[:len(remaining)-len(strings.TrimLeft(remaining, "#0+- "))]
	parser.consumed += len(parser.operation.flags)
}

// parseIndex reads an argument index, the `[2]` of "%[2]d", when one comes next, or is the error that it is
// malformed or not a positive number.
func (parser *formatParser) parseIndex() error {
	text := parser.operation.text
	if parser.consumed == len(text) || text[parser.consumed] != '[' {
		return nil
	}
	parser.consumed++ // The `[`.
	start := parser.consumed
	if number, scanned := parser.scanNumber(); scanned {
		// A `*` or the verb takes it later.
		parser.index = number
		parser.indexPosition = start - 1
	}
	wellFormed := true
	if parser.consumed == len(text) || parser.consumed == start || text[parser.consumed] != ']' {
		// The `]` is missing, or what is before it is no number.
		wellFormed = false
		closing := strings.Index(text[start:], "]")
		if closing < 0 {
			return fmt.Errorf("format %s is missing closing ]", text)
		}
		parser.consumed = closing + start
	}
	argument, parseError := strconv.ParseInt(text[start:parser.consumed], 10, 32)
	if parseError != nil || !wellFormed || argument <= 0 {
		return fmt.Errorf("format has invalid argument index [%s]", text[start:parser.consumed])
	}
	parser.consumed++ // The `]`.
	// The index counts the arguments after the format from 1, and argumentNumber counts the call's from 0.
	parser.argumentNumber = int(argument) + parser.firstArgument - 1
	parser.indexPending = true
	return nil
}

// scanNumber reads a decimal number when one comes next, a size or an index. A number that runs to the end of the
// text is read past and not returned, as fmtstr's scanNum does: the operation then has no verb, which is its error.
func (parser *formatParser) scanNumber() (int, bool) {
	text := parser.operation.text
	start := parser.consumed
	for ; parser.consumed < len(text); parser.consumed++ {
		character := text[parser.consumed]
		if character >= '0' && character <= '9' {
			continue
		}
		if start == parser.consumed {
			return 0, false
		}
		// Ignored: the bytes are all digits, so the only error is a number past 32 bits, and ParseInt then
		// returns the largest one, which fmtstr reads the same way.
		number, _ := strconv.ParseInt(text[start:parser.consumed], 10, 32)
		return int(number), true
	}
	return 0, false
}

// parseSize reads a width, or a precision after its `.`: a `*` that takes it from an argument, an indexed `[n]*`
// when an index is waiting, or a literal number, which printf never reads but the parser still reads past, and is
// emptySize for a size printf has no argument to check for.
func (parser *formatParser) parseSize() formatSize {
	text := parser.operation.text
	if parser.consumed >= len(text) || text[parser.consumed] != '*' {
		// Ignored: a literal size is read past, and fmtstr's value of it is one printf never reads.
		_, _ = parser.scanNumber()
		return emptySize
	}
	parser.consumed++ // The `*`.
	size := formatSize{dynamic: parser.argumentNumber, index: -1}
	if parser.indexPending {
		// The `*` takes the waiting index.
		parser.indexPending = false
		size.index = parser.index
	}
	parser.argumentNumber++
	return size
}

// parsePrecision reads a precision when a `.` comes next, maybe with an index, or is the error of a malformed index.
func (parser *formatParser) parsePrecision() error {
	if parser.consumed >= len(parser.operation.text) || parser.operation.text[parser.consumed] != '.' {
		return nil
	}
	parser.consumed++ // The `.`.
	if indexError := parser.parseIndex(); indexError != nil {
		return indexError
	}
	parser.operation.precision = parser.parseSize()
	return nil
}
