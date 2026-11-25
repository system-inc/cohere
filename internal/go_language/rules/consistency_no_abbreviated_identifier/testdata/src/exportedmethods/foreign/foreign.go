// Package foreign is another package's code, which exportedmethods imports and the test never analyzes. Its only
// method is an exported one on an exported concrete type, which every rule may read.
package foreign

// Ledger's Cmd has the name and signature of exportedmethods' Cmd on its concrete type and its generic type.
type Ledger struct{}

func (Ledger) Cmd(name string) string { return name }
