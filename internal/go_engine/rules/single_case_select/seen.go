package single_case_select

import (
	"go/ast"
)

// Seen is the state one run of the check keeps across the nodes of its preorder, the original's seen set: the selects
// a for statement's finding already names, which the second query must not report again. Its zero value is a run that
// has seen nothing, so whatever hands one out per rule per package hands out a new one.
type Seen struct {
	// selects is every select a for statement's finding names, nil until the first.
	selects map[*ast.SelectStmt]bool
}

// record adds selectStatement to the selects a for statement's finding names.
func (seen *Seen) record(selectStatement *ast.SelectStmt) {
	if seen.selects == nil {
		seen.selects = map[*ast.SelectStmt]bool{}
	}
	seen.selects[selectStatement] = true
}
