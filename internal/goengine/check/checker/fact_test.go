package checker

// declaredFact is what declaredAnalyzer exports for every function a package declares, so an importer can tell a
// call to a function its dependency declared, which it knows only by the fact, from any other call.
type declaredFact struct{}

// AFact makes declaredFact an analysis.Fact.
func (*declaredFact) AFact() {}

// String is how a fact log would print it.
func (*declaredFact) String() string { return "declared" }
