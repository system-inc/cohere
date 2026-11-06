package text_rules

// Result is what the comparator found.
type Result struct {
	// Failure is the kind of failure, or empty when the findings meet the expectation.
	Failure Kind

	// ControlHeld is whether the findings are as many as expected and each one whose span covers text starts
	// on the line that text starts on: the check a control must always pass. A span over the whole file
	// (fileStart, linesFrom) holds its count alone, since where its finding sits is placement.
	ControlHeld bool

	// Detail says what differed, or what matched, in a line a reader can check.
	Detail string
}

// Passed reports whether the findings meet the expectation exactly.
func (result Result) Passed() bool {
	return result.Failure == ""
}
