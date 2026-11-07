package check

// markFact is callsAnalyzer's fact on a function whose body holds the string literal "mark", which an edit of the
// body alone can give or take away, its signature untouched.
type markFact struct{}

// AFact makes markFact an analysis.Fact.
func (*markFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}
