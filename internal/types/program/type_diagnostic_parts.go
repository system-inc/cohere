package program

import "github.com/microsoft/TypeScript/tsc/shim/ast"

// TypeDiagnosticParts is a check's syntactic, bind and semantic diagnostics, kept apart so the semantic
// ones can be recorded per file. With any syntactic diagnostic the check stops there, as the compiler's own
// does, and the other two are empty.
type TypeDiagnosticParts struct {
	Syntactic []*ast.Diagnostic
	Bind      []*ast.Diagnostic
	Semantic  []*ast.Diagnostic
}

// All is the parts in the order AllDiagnostics returns them.
func (parts TypeDiagnosticParts) All() []*ast.Diagnostic {
	if len(parts.Syntactic) > 0 {
		return parts.Syntactic
	}
	return append(append([]*ast.Diagnostic{}, parts.Bind...), parts.Semantic...)
}
