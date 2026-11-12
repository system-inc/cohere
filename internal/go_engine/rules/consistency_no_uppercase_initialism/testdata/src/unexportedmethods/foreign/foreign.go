// Package foreign is another package's code, which unexportedmethods imports and the test never analyzes. Its only
// method is an unexported one on an unexported concrete type, the kind of std type a table's export data leaves out
// and a platform build's source check sees.
package foreign

// ledger's parseURL has the name and signature of unexportedmethods' parseURL on its concrete type and its generic
// type.
type ledger struct{}

func (ledger) parseURL() string { return "" }
