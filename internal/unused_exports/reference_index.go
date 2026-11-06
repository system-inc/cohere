package unused_exports

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// referenceIndex is the edge set: which declarations reference which symbols.
//
// It replaced a plain `map[*ast.Symbol]bool` that recorded only that a symbol was used. The walk is
// identical and costs the same; what changed is what it stores. Recording the edge is what makes
// the transitive question answerable, and the transitive question is the one that finds an island:
// an abandoned export plus the private world that existed only to serve it.
type referenceIndex struct {
	// referencedBy maps a symbol to the declarations that reference it. A symbol present with an
	// empty slice is impossible; a symbol referenced only from module top level appears with
	// `nil` in its slice, which is the root marker rather than a missing entry.
	referencedBy map[*ast.Symbol][]*ast.Symbol

	// referenced is the flat question the original set answered, kept because the flat report is
	// genuinely useful and a reader may trust it more than the closure.
	referenced map[*ast.Symbol]bool
}

// record adds one edge. A nil `from` means the reference sits at module top level, which is a root
// and not an absent referrer: the distinction the whole closure turns on.
func (index *referenceIndex) record(to *ast.Symbol, from *ast.Symbol) {
	index.referenced[to] = true
	index.referencedBy[to] = append(index.referencedBy[to], from)
}
