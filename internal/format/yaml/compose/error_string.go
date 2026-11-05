package compose

// errorString is an Error upstream throws, whose message a caller catches and reports.
type errorString string

func (message errorString) Error() string { return string(message) }
