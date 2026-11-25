package fact_resolver

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_language/intermediate_representation"
)

// Asker is a body whose rule asked a question (#5vbs1np, must 5): its package, and its place, as the facts' reach
// places a body, the file a reader sees past any //line directive and the offset of its `func` in the file as parsed.
// File is empty for a function no declaration's syntax places, as a wrapper the representation makes, which the body
// differential takes as standing, the side that fails.
type Asker struct {
	Package *types.Package
	File    string
	Offset  int
}

// AskerOf is the body function is, or the declaration whose body holds function, a literal of it.
func AskerOf(function *intermediate_representation.Function) Asker {
	outermost := function
	for outermost.Parent() != nil {
		outermost = outermost.Parent()
	}
	var asker Asker
	if outermost.Package != nil {
		asker.Package = outermost.Package.Types
	}
	declaration, isDeclared := outermost.Syntax().(*ast.FuncDecl)
	if !isDeclared || outermost.Program == nil {
		return asker
	}
	if tokenFile := outermost.Program.FileSet.File(declaration.Pos()); tokenFile != nil {
		asker.File = tokenFile.Position(declaration.Pos()).Filename
		asker.Offset = tokenFile.Offset(declaration.Pos())
	}
	return asker
}
