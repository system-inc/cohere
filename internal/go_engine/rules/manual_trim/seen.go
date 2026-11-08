package manual_trim

import (
	"go/ast"
)

// Seen is the state one run of the check keeps across the nodes of its preorder, the original's seen set: the else of
// every if statement that has one, so an else if, which is one branch of a chain, is never judged as a guard of its
// own. Its zero value is a run that has seen nothing, so whatever hands one out per rule per package hands out a new
// one.
type Seen struct {
	// elses is every else statement the preorder passed, a block or an if, nil until the first.
	elses map[ast.Stmt]bool
}

// record adds elseStatement to the elses the preorder passed.
func (seen *Seen) record(elseStatement ast.Stmt) {
	if seen.elses == nil {
		seen.elses = map[ast.Stmt]bool{}
	}
	seen.elses[elseStatement] = true
}
