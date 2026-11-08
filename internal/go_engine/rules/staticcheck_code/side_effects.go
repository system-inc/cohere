// Package staticcheck_code is the questions a check ported from staticcheck asks of Go code, staticcheck's
// analysis/code ported onto rule.Context, one helper at a time as the ports need them, so a port asks them of this
// one copy and never imports staticcheck's framework.
//
// Each helper is translated, not copied, from honnef.co/go/tools@v0.8.1 analysis/code, and answers as the original
// does: a test beside each runs both over every node of a fixture and requires the same answer. Where the original
// reads a pass, a helper reads a rule.Context, and where it reads the result of another of staticcheck's passes, the
// helper takes the narrowest form of it, so a check on the engine's dispatcher asks it as a check on an analyzer does.
package staticcheck_code

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"

	"honnef.co/go/tools/go/types/typeutil"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// MayHaveSideEffects is whether evaluating expression might do more than produce a value: call a function not known
// pure, receive from a channel or take an address. It is staticcheck's code.MayHaveSideEffects.
//
// isPure judges a function pure, and nil judges none pure, so that every call, len and cap among them, may have side
// effects. staticcheck's takes the result of its purity pass, a map nil or not; a check that reads it passes whether
// the map holds the function, and one that passes nil there passes nil here.
func MayHaveSideEffects(context *rule.Context, expression ast.Expr, isPure func(function *types.Func) bool) bool {
	switch expression := expression.(type) {
	case *ast.BadExpr:
		return true
	case *ast.Ellipsis:
		return MayHaveSideEffects(context, expression.Elt, isPure)
	case *ast.FuncLit:
		// A literal has no side effects of its own; only calling it might, which the call answers.
		return false
	case *ast.ArrayType, *ast.StructType, *ast.FuncType, *ast.InterfaceType, *ast.MapType, *ast.ChanType:
		// A type has no side effects.
		return false
	case *ast.BasicLit:
		return false
	case *ast.BinaryExpr:
		return MayHaveSideEffects(context, expression.X, isPure) || MayHaveSideEffects(context, expression.Y, isPure)
	case *ast.CallExpr:
		return callMayHaveSideEffects(context, expression, isPure)
	case *ast.CompositeLit:
		if MayHaveSideEffects(context, expression.Type, isPure) {
			return true
		}
		for _, element := range expression.Elts {
			if MayHaveSideEffects(context, element, isPure) {
				return true
			}
		}
		return false
	case *ast.Ident:
		return false
	case *ast.IndexExpr:
		return MayHaveSideEffects(context, expression.X, isPure) ||
			MayHaveSideEffects(context, expression.Index, isPure)
	case *ast.IndexListExpr:
		// An instantiation with several type arguments names only types, so none of these can have side effects, and
		// staticcheck asks all the same.
		if MayHaveSideEffects(context, expression.X, isPure) {
			return true
		}
		for _, index := range expression.Indices {
			if MayHaveSideEffects(context, index, isPure) {
				return true
			}
		}
		return false
	case *ast.KeyValueExpr:
		return MayHaveSideEffects(context, expression.Key, isPure) ||
			MayHaveSideEffects(context, expression.Value, isPure)
	case *ast.SelectorExpr:
		return MayHaveSideEffects(context, expression.X, isPure)
	case *ast.SliceExpr:
		return MayHaveSideEffects(context, expression.X, isPure) ||
			MayHaveSideEffects(context, expression.Low, isPure) ||
			MayHaveSideEffects(context, expression.High, isPure) ||
			MayHaveSideEffects(context, expression.Max, isPure)
	case *ast.StarExpr:
		return MayHaveSideEffects(context, expression.X, isPure)
	case *ast.TypeAssertExpr:
		return MayHaveSideEffects(context, expression.X, isPure)
	case *ast.UnaryExpr:
		if MayHaveSideEffects(context, expression.X, isPure) {
			return true
		}
		// A receive takes a value from a channel, and taking an address lets the value escape.
		return expression.Op == token.ARROW || expression.Op == token.AND
	case *ast.ParenExpr:
		return MayHaveSideEffects(context, expression.X, isPure)
	case nil:
		// An optional part left out, a slice's missing bound or a composite literal's elided type.
		return false
	default:
		// Invariant: every expression go/ast declares is a case above, so this is an expression type a newer Go
		// added, which the port must learn as staticcheck would.
		panic(fmt.Sprintf("internal error: unhandled type %T", expression))
	}
}

// callMayHaveSideEffects is MayHaveSideEffects of a call: false only for a call of a function isPure judges pure, or
// of len or cap, whose arguments have none, and true for every other call, a conversion or a call of a function
// value among them, whose callee is no function the type checker names.
func callMayHaveSideEffects(context *rule.Context, call *ast.CallExpr, isPure func(function *types.Func) bool) bool {
	if isPure == nil {
		return true
	}
	switch callee := typeutil.Callee(context.TypesInfo, call).(type) {
	case *types.Func:
		if !isPure(callee) {
			return true
		}
	case *types.Builtin:
		if callee.Name() != "len" && callee.Name() != "cap" {
			return true
		}
	default:
		return true
	}
	for _, argument := range call.Args {
		if MayHaveSideEffects(context, argument, isPure) {
			return true
		}
	}
	return false
}
