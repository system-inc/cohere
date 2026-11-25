package check

// plantedFact is the fact type of the analyzers TestAnAnalyzerOnlyALeftOutListenerRequiresNeverRuns plants, which
// they never export. Declaring one is what makes the checker run them on every dependency too, as it runs
// staticcheck's purity and nilness.
type plantedFact struct{}

// AFact makes plantedFact an analysis.Fact.
func (*plantedFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}
