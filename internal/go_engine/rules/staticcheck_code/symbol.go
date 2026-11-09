package staticcheck_code

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IsSymbol is whether expression names the package-level symbol called name, as staticcheck's pattern package matches
// `(Symbol "name")`: an identifier or a selector, parenthesized any number of times, or one instantiated with type
// arguments, `f[int]` or `f[int, string]`, whose object is a function or method whose full name is name
// ("regexp.Compile", "(*sync.WaitGroup).Add"), a builtin function of that name, a type declared at package scope whose
// type prints as name, or a variable or constant declared at package scope of a package whose path and name joined by
// a dot are name. A pattern whose name is an Or, `(Symbol (Or "a" "b"))`, is SymbolIndex's.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 pattern/match.go's Symbol.Match, whose branches it
// keeps one for one, the walk through aliases among them. Where the original builds a function's full name, a type's
// printed form or a variable's path and name to compare with name, the port compares name with their parts in place
// and builds only what it cannot compare that way, which answers the same with no string built for nearly every
// expression, since a port asks this of every call it hears and most are not the symbol.
func IsSymbol(context *rule.Context, expression ast.Expr, name string) bool {
	return SymbolIndex(context, expression, name) == 0
}

// SymbolIndex is which of names expression names, as IsSymbol judges each, the first in their order, or -1 when it
// names none of them: staticcheck's `(Symbol (Or "a" "b"))`, whose Or tries each name in turn. It resolves
// expression's object once, however many names it is asked about.
func SymbolIndex(context *rule.Context, expression ast.Expr, names ...string) int {
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
		return -1
	}
	for index, name := range names {
		if isObjectSymbol(object, name) {
			return index
		}
	}
	return -1
}

// isObjectSymbol is whether object, the object an expression names, is the symbol called name, as IsSymbol judges it.
func isObjectSymbol(object types.Object, name string) bool {
	switch object := object.(type) {
	case *types.Func:
		return isFunctionSymbol(object, name)
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

// isFunctionSymbol is whether function's full name is name, as types.Func's FullName spells it: its name alone when it
// has no type yet, a method's receiver in parentheses, a dot and its name, and any other function's package path, a
// dot and its name, or its name alone when it has no package or its package no path. Only a method's receiver needs
// FullName's printing, and only once name has a method's shape and ends in the method's own name.
func isFunctionSymbol(function *types.Func, name string) bool {
	if function.Type() == nil {
		return name == function.Name()
	}
	if function.Signature().Recv() != nil {
		receiver, isMethodName := strings.CutSuffix(name, function.Name())
		if !isMethodName || !strings.HasPrefix(receiver, "(") || !strings.HasSuffix(receiver, ").") {
			return false
		}
		return function.FullName() == name
	}
	if function.Pkg() == nil || function.Pkg().Path() == "" {
		return name == function.Name()
	}
	return isJoined(name, function.Pkg().Path(), function.Name())
}

// isJoined is whether name is qualifier and local joined by a dot, compared in place.
func isJoined(name string, qualifier string, local string) bool {
	return len(name) == len(qualifier)+1+len(local) && strings.HasPrefix(name, qualifier) &&
		name[len(qualifier)] == '.' && strings.HasSuffix(name, local)
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
		if printsAs(reached.Type(), name) {
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

// printsAs is whether typed prints as name, as types.TypeString spells it with no qualifier. A named type or an alias
// prints as its package path, a dot and its name, or its name alone when it has no package or its package no path,
// followed by its type arguments or type parameters in brackets when it has any; only those brackets need
// TypeString's printing, and only once name begins with the type's own name and a bracket. A basic type prints as its
// name, but for unsafe.Pointer, the one whose name is exported, which TypeString spells through package unsafe. Any
// other type is printed.
func printsAs(typed types.Type, name string) bool {
	var object *types.TypeName
	var isBracketed bool
	switch typed := typed.(type) {
	case *types.Basic:
		if token.IsExported(typed.Name()) {
			return types.TypeString(typed, nil) == name
		}
		return typed.Name() == name
	case *types.Named:
		object = typed.Obj()
		isBracketed = typed.TypeArgs().Len() != 0 || typed.TypeParams().Len() != 0
	case *types.Alias:
		object = typed.Obj()
		isBracketed = typed.TypeArgs().Len() != 0 || typed.TypeParams().Len() != 0
	default:
		return types.TypeString(typed, nil) == name
	}
	unqualified := name
	if object.Pkg() != nil && object.Pkg().Path() != "" {
		inPackage, isInPackage := strings.CutPrefix(name, object.Pkg().Path())
		if !isInPackage {
			return false
		}
		var isQualified bool
		unqualified, isQualified = strings.CutPrefix(inPackage, ".")
		if !isQualified {
			return false
		}
	}
	brackets, isNamed := strings.CutPrefix(unqualified, object.Name())
	if !isNamed {
		return false
	}
	if !isBracketed {
		return brackets == ""
	}
	return strings.HasPrefix(brackets, "[") && types.TypeString(typed, nil) == name
}

// isPackageLevelSymbol is whether object, a variable or constant, is declared at the scope of a package, not in a
// block or as a field, and its package's path and its name joined by a dot are name.
func isPackageLevelSymbol(object types.Object, name string) bool {
	if object.Pkg() == nil || object.Parent() != object.Pkg().Scope() {
		return false
	}
	return isJoined(name, object.Pkg().Path(), object.Name())
}
