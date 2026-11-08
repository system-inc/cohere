package staticcheck_code

import (
	"go/ast"
	"go/token"
	"go/types"

	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// IntegerLiteral is the type and constant value of expression when it is written as an integer literal: a literal of
// integer kind such as 1, 0x1, 0b1, 01 or 1_000, or one under any run of unary plus and minus signs such as -1 or
// -+42, parenthesized anywhere. A named constant, a conversion such as int(1), a character literal, a float literal
// with an integer value such as 1.0, and any other operator such as ^-2 or 2-1 is none, whatever its value. It is
// staticcheck's code.IntegerLiteral, which matches its pattern package's IntegerLiteral; every pattern that names
// IntegerLiteral asks this.
//
// The type and value are those the type checker recorded for expression with its outer parentheses removed, as the
// pattern's are. staticcheck's takes any ast.Node, and its matcher also looks through the statements that wrap an
// expression, but no statement that holds an integer literal alone type-checks, so this takes an expression.
func IntegerLiteral(context *rule.Context, expression ast.Expr) (types.TypeAndValue, bool) {
	unparenthesized := ast.Unparen(expression)
	switch literal := unparenthesized.(type) {
	case *ast.BasicLit:
		if literal.Kind != token.INT {
			return types.TypeAndValue{}, false
		}
	case *ast.UnaryExpr:
		if literal.Op != token.ADD && literal.Op != token.SUB {
			return types.TypeAndValue{}, false
		}
		if _, isLiteral := IntegerLiteral(context, literal.X); !isLiteral {
			return types.TypeAndValue{}, false
		}
	default:
		return types.TypeAndValue{}, false
	}
	// An integer literal under signs is always constant once it type-checks; the pattern asks all the same, and an
	// expression the checker recorded no constant for is none.
	typeAndValue, isRecorded := context.TypesInfo.Types[unparenthesized]
	if !isRecorded || typeAndValue.Value == nil {
		return types.TypeAndValue{}, false
	}
	return typeAndValue, true
}
