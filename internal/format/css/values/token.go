package values

// token is upstream's token array: [type, value, startLine, startColumn, endLine, endColumn, index].
type token struct {
	kind        string
	value       []uint16
	startLine   int
	startColumn int
	endLine     int
	endColumn   int
	// endColumnIsNaN is an end column computed from an undefined `next`.
	endColumnIsNaN bool
	index          int
}
