package checker

// markFact is what markingAnalyzer exports on every object a package declares that can hold a fact, naming the
// object and who marked it, so a reader tells whose fact it found.
type markFact struct {
	Of string
}

// AFact makes markFact an analysis.Fact.
func (*markFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// String is how a summary prints it.
func (fact *markFact) String() string { return "mark of " + fact.Of }
