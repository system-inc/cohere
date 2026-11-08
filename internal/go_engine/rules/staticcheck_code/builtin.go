package staticcheck_code

import (
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsBuiltin is whether expression names the universe's object called name, the builtin function, type, constant or
// nil the language predeclares: an identifier spelled name, parenthesized any number of times, that the type checker
// resolves to the universe's object, so a len, nil or string the code declares itself, in a block or at package
// scope, is not one. It is what staticcheck's pattern package matches for `(Builtin "name")`; a pattern whose name is
// an Or, such as `(Builtin (Or "len" "cap"))`, asks it of each name in turn.
func IsBuiltin(context *rule.Context, expression ast.Expr, name string) bool {
	identifier, isIdentifier := ast.Unparen(expression).(*ast.Ident)
	if !isIdentifier || identifier.Name != name {
		return false
	}
	// The pattern compares the two objects as they are, so a name the universe does not hold, asked of an identifier
	// the checker resolved to nothing, such as the blank identifier or a package clause's name, is one; every name a
	// check asks is the universe's, where that never arises.
	return context.TypesInfo.ObjectOf(identifier) == types.Universe.Lookup(name)
}
