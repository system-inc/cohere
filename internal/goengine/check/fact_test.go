package check

// markedFact is what markedAnalyzer exports for a function named Marked, so a package that imports it can tell
// a call to it from any other call.
type markedFact struct{}

// AFact makes markedFact an analysis.Fact.
func (*markedFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// String is how a fact log would print it.
func (*markedFact) String() string { return "marked" }
