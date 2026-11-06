// Package inert_value decides whether a value written into text another language parses, a shell script or a SQL
// query, can carry that language's syntax. A value whose printed text is only a number or a boolean cannot: digits,
// a sign, a point, an exponent and the words true, false, NaN and Inf are no shell syntax and hold no quote.
//
// The rules that judge a value spliced into code, security-no-interpolated-shell-command and
// security-no-interpolated-sql-string, ask it the same question, so the answer lives once and the two cannot drift:
// a number one rule calls safe is never text the other calls dangerous. What each rule concludes about every other
// value (a string, an interface, a struct) stays its own, since a shell and a SQL string break on different text.
package inert_value

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/types/typeutil"
)

// strconvFormatters return the text of a number or a boolean. Go adds no number to a string, so
// "kill " + strconv.Itoa(pid) is how a number reaches a script or a query, and it is as inert as the number.
// The Append forms are left out: they append to a byte slice the caller hands in, whose contents may be any text.
// FormatComplex is left out too, since it prints parentheses, which a shell parses.
var strconvFormatters = map[string]bool{
	"Itoa": true, "FormatInt": true, "FormatUint": true, "FormatFloat": true, "FormatBool": true,
}

// depthLimit bounds the walk through a type parameter's constraint and the interfaces it embeds, which no written
// constraint nests this deep.
const depthLimit = 16

// Is is whether expression's value prints only as a number or a boolean: a call to one of strconv's formatters, or a
// value whose type is a number or a boolean, a type parameter included, that does not print through a method of its
// own.
func Is(info *types.Info, expression ast.Expr) bool {
	if call, isCall := ast.Unparen(expression).(*ast.CallExpr); isCall {
		function, isFunc := typeutil.Callee(info, call).(*types.Func)
		if isFunc && function.Pkg() != nil && function.Pkg().Path() == "strconv" &&
			function.Signature().Recv() == nil && strconvFormatters[function.Name()] {
			return true
		}
	}
	valueType := info.TypeOf(expression)
	return valueType != nil && isInertType(valueType)
}

// isInertType is whether every value of candidate prints as a number or a boolean.
func isInertType(candidate types.Type) bool {
	if printsThroughMethod(candidate) {
		return false
	}
	// A type parameter's Underlying is its constraint, an interface, so it is asked about its type set first.
	if parameter, isParameter := types.Unalias(candidate).(*types.TypeParam); isParameter {
		return onlyNumbersAndBooleans(parameter.Constraint(), 0)
	}
	return isNumberOrBoolean(candidate)
}

// printsThroughMethod is whether fmt prints a value of this type through a method of its own, String, Error, Format
// or GoString, rather than as the number or boolean it holds. fmt asks the value itself, so only the type's own
// method set counts, not its pointer's. A type parameter's method set is its constraint's.
func printsThroughMethod(candidate types.Type) bool {
	methods := types.NewMethodSet(candidate)
	for index := range methods.Len() {
		switch methods.At(index).Obj().Name() {
		case "String", "Error", "Format", "GoString":
			return true
		}
	}
	return false
}

// onlyNumbersAndBooleans is whether constraint limits a type set to numbers and booleans: some element of it, a
// union or a single type, has nothing else in it. An embedded interface narrows the set too, so it is asked the same.
func onlyNumbersAndBooleans(constraint types.Type, depth int) bool {
	bounds, isInterface := constraint.Underlying().(*types.Interface)
	if !isInterface || depth > depthLimit {
		return false
	}
	for index := range bounds.NumEmbeddeds() {
		switch element := bounds.EmbeddedType(index).(type) {
		case *types.Union:
			fixed := true
			for termIndex := range element.Len() {
				fixed = fixed && isNumberOrBoolean(element.Term(termIndex).Type())
			}
			if fixed {
				return true
			}
		default:
			if isNumberOrBoolean(element) || onlyNumbersAndBooleans(element, depth+1) {
				return true
			}
		}
	}
	return false
}

// isNumberOrBoolean is whether candidate is a number or a boolean, a named one included.
func isNumberOrBoolean(candidate types.Type) bool {
	basic, isBasic := candidate.Underlying().(*types.Basic)
	return isBasic && basic.Info()&(types.IsNumeric|types.IsBoolean) != 0
}
