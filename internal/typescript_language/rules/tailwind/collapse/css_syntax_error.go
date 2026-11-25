package tailwind

import "fmt"

// CssSyntaxError is a malformed-stylesheet error.
//
// Upstream carries a SourceLocation so it can render `file:line:column`. This port carries the byte
// offset instead: cohere reports on the class literal in a user's source file, never on CSS it
// parsed, so an offset is enough to identify the input that failed and a line table would be
// machinery with no reader. The message text matches upstream so a failure here is greppable
// against the upstream source that produced it.
type CssSyntaxError struct {
	// Message is the upstream message text, without any location prefix.
	Message string
	// Offset is the byte offset into the parsed input where the error was detected.
	Offset int
}

func (syntaxError *CssSyntaxError) Error() string {
	return fmt.Sprintf("%d: %s", syntaxError.Offset, syntaxError.Message)
}
