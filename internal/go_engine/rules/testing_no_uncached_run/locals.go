package testing_no_uncached_run

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// locals is what the rule knows about the package's local variables, gathered once so tracing a spread slice to its
// definition costs a map lookup.
type locals struct {
	// definitions is the expression each local was given where it was declared, when it was declared with one.
	definitions map[*types.Var]ast.Expr
	// written holds every local assigned again after its declaration, or whose elements or address are written, so
	// its declared value is no longer known to be what reaches the command.
	written map[*types.Var]bool
}

// sequence is the elements of a slice expression spread into a command, when the rule can read them: a []string
// literal, a call to the builtin append on a slice it can read, or a local declared with one of those and never
// written since. Anything else, a slice from a call, a parameter or a package variable, is unreadable.
func (known locals) sequence(pass *analysis.Pass, expression ast.Expr) ([]argument, bool) {
	switch shape := ast.Unparen(expression).(type) {
	case *ast.CompositeLit:
		var elements []argument
		for _, element := range shape.Elts {
			// A keyed element places itself by index, so the literal's written order is not the slice's.
			if _, isKeyed := element.(*ast.KeyValueExpr); isKeyed {
				return nil, false
			}
			elements = append(elements, argumentOf(pass, element))
		}
		return elements, true
	case *ast.CallExpr:
		return known.appended(pass, shape)
	case *ast.Ident:
		variable, isVariable := pass.TypesInfo.Uses[shape].(*types.Var)
		if !isVariable || variable.Parent() == pass.Pkg.Scope() || known.written[variable] {
			return nil, false
		}
		definition, isDefined := known.definitions[variable]
		if !isDefined {
			return nil, false
		}
		return known.sequence(pass, definition)
	}
	return nil, false
}

// appended reads a call to the builtin append whose first argument is a slice the rule can read. What it appends is
// read too, and a spread it cannot read holds one unknown place, since it can add arguments but not remove the
// constants before it.
func (known locals) appended(pass *analysis.Pass, call *ast.CallExpr) ([]argument, bool) {
	identifier, isIdentifier := ast.Unparen(call.Fun).(*ast.Ident)
	if !isIdentifier || len(call.Args) == 0 {
		return nil, false
	}
	builtin, isBuiltin := pass.TypesInfo.Uses[identifier].(*types.Builtin)
	if !isBuiltin || builtin.Name() != "append" {
		return nil, false
	}
	elements, readable := known.sequence(pass, call.Args[0])
	if !readable {
		return nil, false
	}
	added := call.Args[1:]
	if call.Ellipsis.IsValid() && len(added) == 1 {
		spread, spreadReadable := known.sequence(pass, added[0])
		if !spreadReadable {
			return append(elements, argument{node: added[0]}), true
		}
		return append(elements, spread...), true
	}
	for _, expression := range added {
		elements = append(elements, argumentOf(pass, expression))
	}
	return elements, true
}
