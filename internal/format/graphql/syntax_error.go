package graphql

import "fmt"

// SyntaxError is graphql-js's GraphQLError from syntaxError(source, position, description), as Prettier
// reports it: createParseError reads the error's first location into Prettier's createError, whose
// message appends " (line:column)".
type SyntaxError struct {
	// Description is syntaxError's description, without graphql-js's "Syntax Error: " prefix.
	Description string
	// Position is the byte offset in the text where graphql-js's error points.
	Position int
	// Line and Column are graphql-js's error.locations[0]: 1-indexed, the column in UTF-16 code units.
	Line   int
	Column int
}

// Error is Prettier's message for the error: GraphQLError's message, then the location.
func (syntaxError *SyntaxError) Error() string {
	return fmt.Sprintf("Syntax Error: %s (%d:%d)", syntaxError.Description, syntaxError.Line, syntaxError.Column)
}
