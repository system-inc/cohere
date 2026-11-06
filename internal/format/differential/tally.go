package differential

// Tally counts outcomes for one language.
type Tally struct {
	Identical        int
	Different        int
	CandidateRefused int
	OracleFailed     int

	// RewriteIdentical and RewriteTotal are the same count restricted to files the oracle rewrites.
	RewriteIdentical int
	RewriteTotal     int
}

// Measured is the denominator: every file with a correct answer to match.
func (tally Tally) Measured() int {
	return tally.Identical + tally.Different + tally.CandidateRefused
}

// Percent is the identity rate over measured files. A tally with nothing measured reports zero rather
// than dividing by zero or claiming a perfect score over an empty set.
func (tally Tally) Percent() float64 {
	if tally.Measured() == 0 {
		return 0
	}
	return 100 * float64(tally.Identical) / float64(tally.Measured())
}

// RewritePercent is the identity rate over files the oracle rewrites.
func (tally Tally) RewritePercent() float64 {
	if tally.RewriteTotal == 0 {
		return 0
	}
	return 100 * float64(tally.RewriteIdentical) / float64(tally.RewriteTotal)
}
