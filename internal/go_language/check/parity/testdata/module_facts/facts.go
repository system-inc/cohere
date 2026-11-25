// Package facts holds the functions the planted fact analyzers mark over a whole tree: exported ones, an unexported
// one, which no importer inherits a fact about, and one named `_`, which has no objectpath.
package facts

func First() {}

func Second() {}

func third() {}

func _() {}
