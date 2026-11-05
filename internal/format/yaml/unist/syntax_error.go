package unist

// SyntaxError is upstream's YAMLSyntaxError: the first error of the first document that has one, with
// the message and code eemeli/yaml gave it and its position in the text. Position is in Node.Position's
// units: byte offsets, 1-based lines, columns in UTF-16 units.
type SyntaxError struct {
	Message  string
	Code     string
	Position Position
}

func (syntaxError *SyntaxError) Error() string { return syntaxError.Message }
