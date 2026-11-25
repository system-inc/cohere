package gate

// Loose writes interface{} in a local variable's type, a candidate of its own that no use records.
func Loose() bool {
	var held interface{} = 1
	return held != nil
}

// Port writes no candidate.
func Port() int { return 80 }
