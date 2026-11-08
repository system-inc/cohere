package signal_channel_analyzer

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/ast/inspector"
)

// declaredValues maps each variable declared with a value of its own, by `:=` or by `var`, to that value, across the
// package's files. It stands in for the original's findDecl, which reads the declaration the parser resolved an
// identifier to (ast.Ident.Obj), and keeps findDecl's judgment: a declaration whose names and values differ in number,
// `channel, received := <-channels`, gives its names no value. The parser resolves a `:=` that reuses a variable to
// the variable's first declaration, and the type checker defines nothing there, so the two agree. Neither a range
// clause nor a type switch's header is a declaration with a value here; findDecl reaches them as a range or a type
// assertion, which is never the call either judgment looks for.
type declaredValues map[types.Object]ast.Expr

// declaredValuesOf reads every `:=` and every `var` specification in the package inspected walks.
func declaredValuesOf(inspected *inspector.Inspector, information *types.Info) declaredValues {
	declarations := declaredValues{}
	record := func(name ast.Expr, value ast.Expr) {
		identifier, isIdentifier := name.(*ast.Ident)
		if !isIdentifier {
			return
		}
		// Defs holds no object for a name a `:=` reuses rather than declares, which keeps its first declaration.
		if object := information.Defs[identifier]; object != nil {
			declarations[object] = value
		}
	}
	for assignment := range inspector.All[*ast.AssignStmt](inspected) {
		// findDecl gives nothing for a declaration whose sides differ in length. An `=` declares nothing, so Defs holds
		// none of its names and record passes them over.
		if len(assignment.Lhs) != len(assignment.Rhs) {
			continue
		}
		for index, name := range assignment.Lhs {
			record(name, assignment.Rhs[index])
		}
	}
	for specification := range inspector.All[*ast.ValueSpec](inspected) {
		if len(specification.Names) != len(specification.Values) {
			continue
		}
		for index, name := range specification.Names {
			record(name, specification.Values[index])
		}
	}
	return declarations
}

// valueOf is the value the variable identifier uses was declared with, or nil when it names no variable declared with
// one in this package.
func (declarations declaredValues) valueOf(information *types.Info, identifier *ast.Ident) ast.Expr {
	return declarations[information.Uses[identifier]]
}
