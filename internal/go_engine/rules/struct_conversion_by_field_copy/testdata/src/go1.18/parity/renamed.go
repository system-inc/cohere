package parity

import (
	renamed "example.com/parity/other"
)

// A renamed import of another package, whose name the fix writes as the literal does.
func renamedImport() {
	couple := renamed.Couple{}
	_ = renamed.Pair{couple.A, couple.B}         // reported
	_ = (renamed.Pair{A: couple.A, B: couple.B}) // reported
}
