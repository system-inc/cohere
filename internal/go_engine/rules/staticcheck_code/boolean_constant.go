//cohere:file-ignore consistency-require-separated-folder-words staticcheck's name, a project's own spelling
package staticcheck_code

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// BoolConst is the value of the boolean constant expression names. It is staticcheck's code.BoolConst, from
// honnef.co/go/tools@v0.8.1 analysis/code/code.go, and asks no more than it: a caller asks IsBoolConst first, and
// expression must be an identifier naming a constant whose value is a boolean, of any boolean type, or it panics.
func BoolConst(context *rule.Context, expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier {
		// Invariant: a caller asks IsBoolConst first, which holds only of an identifier.
		panic(fmt.Sprintf("BoolConst of %T, which is no identifier", expression))
	}
	named, isConstant := context.TypesInfo.ObjectOf(identifier).(*types.Const)
	if !isConstant {
		// Invariant: a caller asks IsBoolConst first, which holds only of an identifier naming a constant.
		panic(fmt.Sprintf("BoolConst of %s, which names no constant", identifier.Name))
	}
	// constant.BoolVal panics for a value that is no boolean, as staticcheck's does.
	return constant.BoolVal(named.Val())
}

// IsBoolConst is whether expression is an identifier naming a constant of type bool, or an untyped boolean constant
// such as true. It is staticcheck's code.IsBoolConst, from honnef.co/go/tools@v0.8.1 analysis/code/code.go.
//
// A constant of a named boolean type is none, and neither is one declared with an alias of bool, which staticcheck
// treats as it treats a named type: a custom boolean is most often a two-valued enumeration, whose explicit
// comparison is what its author meant, so a check that asks this errs toward saying nothing.
func IsBoolConst(context *rule.Context, expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	if !isIdentifier {
		return false
	}
	named, isConstant := context.TypesInfo.ObjectOf(identifier).(*types.Const)
	if !isConstant {
		return false
	}
	// The type is not unaliased, so a constant declared with an alias of bool is no basic type here.
	basic, isBasic := named.Type().(*types.Basic)
	if !isBasic {
		return false
	}
	return basic.Kind() == types.UntypedBool || basic.Kind() == types.Bool
}
