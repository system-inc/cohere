package correctness_require_wrapped_error

import "strings"

// directive is one verb of a format string and the argument it formats.
type directive struct {
	// text is the directive as written, `%[2]+v`, for the message.
	text string
	verb rune
	// sharp is the # flag, which turns %v into Go syntax rather than the error's text.
	sharp bool
	// argument is the index into the call's arguments after the format.
	argument int
}

// printsText is whether fmt answers this directive with an error's Error() text, the one output %w reproduces.
// %#v prints the error as a Go value, a debugging view %w would replace with its text, so it is no lost wrap.
func (each directive) printsText() bool {
	return strings.ContainsRune(textVerbs, each.verb) && !(each.verb == 'v' && each.sharp)
}
