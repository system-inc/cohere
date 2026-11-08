package staticcheck_code

import (
	"fmt"
	"go/ast"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsSymbol is whether expression names the package-level symbol called name, as staticcheck's pattern package matches
// `(Symbol "name")`: an identifier or a selector, parenthesized any number of times, or one instantiated with type
// arguments, `f[int]` or `f[int, string]`, whose object is a function or method whose full name is name
// ("regexp.Compile", "(*sync.WaitGroup).Add"), a builtin function of that name, a type declared at package scope whose
// type prints as name, or a variable or constant declared at package scope of a package whose path and name joined by
// a dot are name. A pattern whose name is an Or asks it of each name in turn.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 pattern/match.go's Symbol.Match, whose branches it
// keeps one for one, the walk through aliases among them.
func IsSymbol(context *rule.Context, expression ast.Expr, name string) bool {
	named := ast.Unparen(expression)
	switch instantiated := named.(type) {
	case *ast.IndexExpr:
		named = ast.Unparen(instantiated.X)
	case *ast.IndexListExpr:
		named = ast.Unparen(instantiated.X)
	}
	var object types.Object
	switch named := named.(type) {
	case *ast.Ident:
		object = context.TypesInfo.ObjectOf(named)
	case *ast.SelectorExpr:
		object = context.TypesInfo.ObjectOf(named.Sel)
	default:
		return false
	}
	switch object := object.(type) {
	case *types.Func:
		return object.FullName() == name
	case *types.Builtin:
		return object.Name() == name
	case *types.TypeName:
		return isTypeSymbol(object, name)
	case *types.Const:
		return isPackageLevelSymbol(object, name)
	case *types.Var:
		return isPackageLevelSymbol(object, name)
	}
	return false
}

// isTypeSymbol is whether typeName, declared at package scope, prints as name, or, when it is an alias that does not,
// whether the type it stands for does: a named type through its own name, at package scope too, and a basic type
// through its name alone. A universe type such as int counts as declared at package scope, since go/types gives it
// the universe as both its parent and its package's scope.
func isTypeSymbol(typeName *types.TypeName, name string) bool {
	// The type name the walk has reached, typeName first and then each named type an alias stands for.
	reached := typeName
	for {
		if reached.Parent() != reached.Pkg().Scope() {
			return false
		}
		if types.TypeString(reached.Type(), nil) == name {
			return true
		}
		if !reached.IsAlias() {
			return false
		}
		// The original peels every alias at once where it would peel one at a time, blocked on golang/go#66559, and
		// the port keeps its walk.
		switch aliased := types.Unalias(reached.Type()).(type) {
		case interface{ Obj() *types.TypeName }:
			reached = aliased.Obj()
		case *types.Basic:
			return aliased.Name() == name
		default:
			return false
		}
	}
}

// isPackageLevelSymbol is whether object, a variable or constant, is declared at the scope of a package, not in a
// block or as a field, and its package's path and its name joined by a dot are name.
func isPackageLevelSymbol(object types.Object, name string) bool {
	if object.Pkg() == nil || object.Parent() != object.Pkg().Scope() {
		return false
	}
	return fmt.Sprintf("%s.%s", object.Pkg().Path(), object.Name()) == name
}
