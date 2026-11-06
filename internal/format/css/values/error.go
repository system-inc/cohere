package values

// Error is a throw from the library: a ParserError, a TokenizeError, or the TypeError upstream hits
// reading a property of undefined. Error() is JavaScript's String(error), "Name: message".
type Error struct {
	Name    string
	Message string
}

func (thrown *Error) Error() string { return thrown.Name + ": " + thrown.Message }
