package unist

// ThrownError is an Error yaml-unist-parser throws that is not a YAMLSyntaxError: one of its own
// "Unexpected ..." checks (Name "Error"), or a TypeError the JavaScript engine raises where upstream
// reads a property of undefined or null (on a !!pairs or !!omap sequence, say), whose message is V8's.
type ThrownError struct {
	Name    string
	Message string
}

func (thrown *ThrownError) Error() string { return thrown.Name + ": " + thrown.Message }
