// Package foreign stands in for a package we do not own, which unexportedmethods imports and the test never analyzes.
// Its only method is an unexported one on an unexported concrete type, the kind of std type a table's export data
// leaves out and a platform build's source check sees.
package foreign

// ledger's cmd has the name and signature of unexportedmethods' cmd on its concrete type and its generic type.
type ledger struct{}

func (ledger) cmd(name string) string { return name }
