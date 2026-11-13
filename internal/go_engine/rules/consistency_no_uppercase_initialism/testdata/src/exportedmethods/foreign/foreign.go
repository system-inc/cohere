// Package foreign is another package's code, which exportedmethods imports and the test never analyzes. Its only
// method is an exported one on an exported concrete type, which every rule may read.
package foreign

// Ledger's ParseURL has the name and signature of exportedmethods' ParseURL on its concrete type and its generic type.
type Ledger struct{}

func (Ledger) ParseURL() string { return "" }
