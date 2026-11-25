package check

// learnerFact is failingLearner's fact type, which it never exports: an analyzer that learns facts must name one.
type learnerFact struct{}

// AFact marks learnerFact a fact.
func (*learnerFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}
