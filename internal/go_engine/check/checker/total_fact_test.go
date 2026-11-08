package checker

import "fmt"

// totalFact is what totalAnalyzer exports about a package: how many functions it and every package below it
// declare, counted along each import.
type totalFact struct {
	Functions int
}

// AFact makes totalFact an analysis.Fact.
func (*totalFact) AFact() {
	// Nothing to do: analysis.Fact is a marker interface, and having the method is what it asks.
}

// String is how the results test prints it.
func (fact *totalFact) String() string { return fmt.Sprintf("%d functions", fact.Functions) }
