package printing

import "errors"

// Syntax marks err as the source failing to parse, keeping its message.
//
// The format phase treats a file that does not parse as a skip and every other failure as a broken
// formatter, so it has to tell the two apart. It used to tell by matching the goja engine's messages,
// which belong to upstream and differ by parser: JSON's "unexpected" was read as a broken formatter.
// A parser that wraps its failures here is recognized whatever its wording.
func Syntax(sourceError error) error {
	if sourceError == nil {
		return nil
	}
	return syntaxError{sourceError}
}

// IsSyntax reports whether err, or anything it wraps, was marked by Syntax.
func IsSyntax(candidateError error) bool {
	var marked syntaxError
	return errors.As(candidateError, &marked)
}
