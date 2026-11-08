package printf

import (
	"errors"
	"strings"
)

// formatOperation is one parsed printf operation, such as "%3.*[4]d": x/tools' fmtstr.Operation. fmtstr also records
// each size's literal value and where the size sits, which printf never reads, so the translation leaves them out.
type formatOperation struct {
	// text is the operation's whole text, such as "%[2]*.3d".
	text string

	// verb is the operation's verb, which every operation the parser returns has.
	verb formatVerb

	// byteRange is where text sits in the whole format string, in bytes.
	byteRange byteRange

	// flags are the operation's flags, such as "-0", a subset of "#0+- ".
	flags string

	// width and precision are the operation's sizes, each one maybe taken from an argument.
	width     formatSize
	precision formatSize
}

// formatSize is a width or a precision as printf reads it: the argument a `*` takes it from, and the index written
// before that `*`, each -1 when the size has none.
type formatSize struct {
	// dynamic is the index in the call's arguments of the argument a `*` takes the size from, or -1.
	dynamic int

	// index is n of an indexed `[n]*`, or -1.
	index int
}

// formatVerb is an operation's verb: the rune, where it sits, the index written before it, and the argument it
// formats.
type formatVerb struct {
	verb rune

	// byteRange is where the verb sits in the format string, its `[n]` included when it has one.
	byteRange byteRange

	// index is n of an indexed `%[n]d`, or -1.
	index int

	// argumentIndex is the index in the call's arguments of the argument the verb formats.
	argumentIndex int
}

// byteRange is a span of bytes in the format string, start included and end not.
type byteRange struct {
	start int
	end   int
}

// emptySize is a size the operation does not have.
var emptySize = formatSize{dynamic: -1, index: -1}

// parseFormat parses every operation of format, the constant value of the argument at formatIndex in the call, or
// is the error of the first one that does not parse: x/tools' fmtstr.Parse. Every error is in predicate form, "call
// has a problem", so it reads after the called function's name. It judges neither the verbs nor whether the call
// has the arguments they read.
func parseFormat(format string, formatIndex int) ([]*formatOperation, error) {
	if !strings.Contains(format, "%") {
		return nil, errors.New("call has arguments but no formatting directives")
	}
	firstArgument := formatIndex + 1
	argumentNumber := firstArgument
	var operations []*formatOperation
	for index, width := 0, 0; index < len(format); index += width {
		width = 1
		if format[index] != '%' {
			continue
		}
		parser, parseError := parseOperation(format[index:], firstArgument, argumentNumber)
		if parseError != nil {
			return nil, parseError
		}
		parser.operation.addOffset(index)
		operations = append(operations, parser.operation)
		width = len(parser.operation.text)
		// A `%%` formats no argument, so it uses none up.
		if parser.operation.verb.verb != '%' {
			argumentNumber = parser.argumentNumber + 1
		}
	}
	return operations, nil
}

// addOffset moves the operation's positions, which parseOperation gives from the operation's own start, to their
// place in the whole format string, at offset.
func (operation *formatOperation) addOffset(offset int) {
	operation.verb.byteRange.start += offset
	operation.verb.byteRange.end += offset
	operation.byteRange.start = offset
	operation.byteRange.end = operation.verb.byteRange.end
}
