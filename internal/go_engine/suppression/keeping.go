package suppression

// Keeping is one directive naming a rule, as Index.Keepings reads it: where it sits, the line it covers, or its whole
// file, and the reason it gives.
type Keeping struct {
	File string

	// Line is where the directive sits, 1-based.
	Line int

	WholeFile bool

	// CoveredLine is the one line a line directive covers: the next line for a comment on its own line, its own line
	// for a comment after code.
	CoveredLine int

	Reason string
}

// Covers reports whether the directive covers a line of its file.
func (keeping Keeping) Covers(line int) bool {
	return keeping.WholeFile || keeping.CoveredLine == line
}
