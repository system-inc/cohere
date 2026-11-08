package staticcheck_code

import (
	"go/ast"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// CallName is the name of the function call calls, as staticcheck spells it to compare against the names it knows:
// "fmt.Println" for a package's function, "(*bytes.Buffer).Write" for a method, "len" for a builtin, and "" for a
// call of anything else, a conversion, a function value, a closure or a builtin of package unsafe among them. It is
// staticcheck's code.CallName, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// A method is named by the receiver its declaration gives it, not the type it is called through, so a call through
// an alias, an embedding struct or an interface embedding another names the method where it was declared, and an
// explicitly instantiated generic function is named as the generic one.
func CallName(context *rule.Context, call *ast.CallExpr) string {
	callee := ast.Unparen(call.Fun)
	// An instantiation cannot yield another generic function, so one is all a callee can carry, and no
	// parentheses can stand inside it, since the type checker refuses (f)[T] as a use of f uninstantiated.
	switch instantiation := callee.(type) {
	case *ast.IndexExpr:
		callee = instantiation.X
	case *ast.IndexListExpr:
		callee = instantiation.X
	}
	switch callee := callee.(type) {
	case *ast.SelectorExpr:
		// A builtin of package unsafe, such as unsafe.Sizeof, is a selector whose object is no function, and has no
		// name here, as staticcheck's has none.
		function, isFunction := context.TypesInfo.ObjectOf(callee.Sel).(*types.Func)
		if !isFunction {
			return ""
		}
		return typeutil.FuncName(function)
	case *ast.Ident:
		switch object := context.TypesInfo.ObjectOf(callee).(type) {
		case *types.Func:
			return typeutil.FuncName(object)
		case *types.Builtin:
			return object.Name()
		default:
			return ""
		}
	default:
		return ""
	}
}
