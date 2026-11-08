//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling

package staticcheck_call

import "honnef.co/go/tools/go/ir"

// Argument is one argument of a Call.
type Argument struct {
	// Value is the argument, as it was before any conversion to an interface the parameter's type made. The
	// original wraps it in a struct of this one field; nothing reads the wrapper but to unwrap it.
	Value ir.Value

	// invalids are the messages the Rule marked the argument with.
	invalids []string
}

// Invalid marks the argument invalid, reported over the argument's expression with message.
func (argument *Argument) Invalid(message string) {
	argument.invalids = append(argument.invalids, message)
}
