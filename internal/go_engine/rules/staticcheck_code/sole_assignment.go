package staticcheck_code

import (
	"go/ast"
)

// SoleAssignment is the assignment a list of one statement holds, looking through labels and blocks of one
// statement on the way, as honnef.co/go/tools@v0.8.1 pattern/match.go looks through them before comparing a
// statement with a pattern's AssignStmt, so a port whose query matches a body of `[(AssignStmt ...)]` asks this of
// the body's list. A list of any other length, an empty block among them, holds none. No code helper of
// staticcheck's spells it.
func SoleAssignment(statements []ast.Stmt) (*ast.AssignStmt, bool) {
	if len(statements) != 1 {
		return nil, false
	}
	statement := statements[0]
	for {
		switch wrapped := statement.(type) {
		case *ast.AssignStmt:
			return wrapped, true
		case *ast.LabeledStmt:
			statement = wrapped.Stmt
		case *ast.BlockStmt:
			if len(wrapped.List) != 1 {
				return nil, false
			}
			statement = wrapped.List[0]
		default:
			return nil, false
		}
	}
}
