package tailwind

// recordedParser answers from what the real engine said, which is what makes this a test of the
// gate's bucketing rather than a test of a Go parser that does not exist.
//
// Parsing a class correctly needs the theme, so the gate deliberately takes the parse as input. The
// question these fixtures answer is the one the gate actually owns: given true readings, does the
// bucketing keep every collapse?
type recordedParser struct {
	candidates map[string][]Candidate
	declared   map[string][]string
}

func (parser recordedParser) ParseCandidates(className string) []Candidate {
	return parser.candidates[className]
}

func (parser recordedParser) DeclaredValues(className string) []string {
	return parser.declared[className]
}
