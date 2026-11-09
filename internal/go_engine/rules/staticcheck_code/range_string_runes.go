package staticcheck_code

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/core_type"
	"github.com/system-inc/cohere/internal/go_engine/intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rule"
)

// rangeStringRunesMessage is the original's message, word for word.
const rangeStringRunesMessage = "should range over string, not []rune(string)"

// RangeStringRunes judges one function of the engine's own intermediate representation: each range statement of its
// syntax that drops the index and ranges over a string converted to a slice of runes, where nothing else reads the
// slice, so ranging over the string itself yields the same runes without allocating. It is a Function callback of
// rule.IntermediateRepresentationListener, and it is staticcheck's sharedcheck.CheckRangeStringRunes, the one function
// staticcheck's SA6003 and S1029 both run, translated, not copied, from honnef.co/go/tools@v0.8.1
// internal/sharedcheck/lint.go, which cohere cannot import.
//
// The original walks every source function of a package in one run, but keeps nothing from one function to the next,
// so judging one at a time finds the same. It reports with the pass's Reportf, at the start of the range statement and
// with no end, and so does this. It walks the function's whole syntax, the function literals within it included, and
// asks the function for the value of each range's operand; a literal is a function of its own, whose operands the
// enclosing function holds no value for, so each range is judged once, by the function whose body holds it.
func RangeStringRunes(context *rule.Context, function *intermediate_representation.Function) {
	source := function.Source()
	if source == nil {
		// A function staticcheck synthesized, a package's initializer among them, has no syntax to walk.
		return
	}
	ast.Inspect(source, func(node ast.Node) bool {
		rangeStatement, isRange := node.(*ast.RangeStmt)
		if !isRange || !isBlank(rangeStatement.Key) {
			return true
		}
		// The operand's value is the function's own only when the range is in its body, not in a literal within it.
		operand, _ := function.ValueForExpression(rangeStatement.X)
		conversion, isConversion := operand.(*intermediate_representation.Convert)
		if !isConversion || !isStringToRunes(conversion) {
			return true
		}
		// Ranging over a slice reads it twice, once for its length and once for each element. Any other reader, the
		// slice ranged over twice among them, as the original's own comment owns, leaves it alone.
		referrers := conversion.Referrers()
		if referrers == nil || len(*referrers) != 2 {
			return true
		}
		context.Report(analysis.Diagnostic{Pos: rangeStatement.Pos(), Message: rangeStringRunesMessage})
		return true
	})
}

// isBlank is whether expression is the blank identifier, as staticcheck's astutil.IsBlank judges it; a range with no
// index at all has no expression there, and is not blank.
func isBlank(expression ast.Expr) bool {
	identifier, isIdentifier := expression.(*ast.Ident)
	return isIdentifier && identifier.Name == "_"
}

// isStringToRunes is whether conversion converts a string to a slice of runes, judged by the core types, so a type
// parameter constrained to strings or to slices of runes counts as one.
func isStringToRunes(conversion *intermediate_representation.Convert) bool {
	source, isBasic := core_type.Of(conversion.X.Type()).(*types.Basic)
	if !isBasic || source.Kind() != types.String {
		return false
	}
	destination, isSlice := core_type.Of(conversion.Type()).(*types.Slice)
	if !isSlice {
		return false
	}
	element, isBasicElement := types.Unalias(destination.Elem()).(*types.Basic)
	return isBasicElement && element.Kind() == types.Int32
}
