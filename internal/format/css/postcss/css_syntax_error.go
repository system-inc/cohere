package postcss

import "strconv"

// CssSyntaxError is lib/css-syntax-error.js's error as the parser throws it, with the input fields
// Input.error attaches (offset and endOffset, here in bytes).
//
// Prettier reads name, reason, line and column from it to build its own error
// (`${name}: ${reason}` at { line, column }), so those are kept exactly; Column and EndColumn are in
// UTF-16 units, as postcss counts them.
type CssSyntaxError struct {
	// Reason is error.reason, the message without the position.
	Reason string
	// Line and Column are where the problem starts, one-based.
	Line   int
	Column int
	// EndLine and EndColumn are where it ends, when HasEnd.
	EndLine   int
	EndColumn int
	// Offset and EndOffset are error.input.offset and error.input.endOffset, in bytes.
	Offset    int
	EndOffset int
	HasEnd    bool
}

// Name is error.name.
func (*CssSyntaxError) Name() string { return "CssSyntaxError" }

// Error is setMessage's message: no plugin, no file, so "<css input>:line:column: reason".
func (syntaxError *CssSyntaxError) Error() string {
	return "<css input>:" +
		strconv.Itoa(syntaxError.Line) +
		":" +
		strconv.Itoa(syntaxError.Column) +
		": " +
		syntaxError.Reason
}
