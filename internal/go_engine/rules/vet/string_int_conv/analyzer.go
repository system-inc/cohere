// Package string_int_conv is vet's stringintconv pass, a conversion `string(x)` of an integer x that is neither a
// byte nor a rune, which yields the UTF-8 encoding of the code point x, a string of one rune, where the writer most
// likely meant x's decimal digits. A conversion to a type parameter, or of a value of one, is judged over the type
// sets: it is reported when some type in the target's set is a string and some type in the source's set is such an
// integer. A finding offers to format the number with fmt.Sprint, importing fmt where the file needs it, when each
// side's set holds one type, the source's type has no methods, and the fix keeps the conversion's type, and to
// convert the value to a rune first, when every type in the source's set converts to one.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/stringintconv/string.go, with the
// helpers it calls from x/tools' internal packages (typeparams.StructuralTerms, typesinternal.TypeNameFor and
// refactor.AddImport) translated beside it, and it reports the original's messages word for word, at the same
// position. Its suggested fixes are the original's, edit for edit, but in two places, where the port withholds the
// original's "Format the number as a decimal" because applying it would be wrong. One is a conversion to a type
// parameter whose one term is string, `AllString(x)`, which the fix would replace with `fmt.Sprint(x)`, a string, not
// an AllString. The other is a value of a type parameter whose constraint gives it a Format, Error or String method,
// which fmt.Sprint would print through in place of the number; the original reads only the methods of the
// constraint's one term. The registry runs the port in the original's place, and the original stays the oracle: the
// parity tests beside this file run both over the same packages and compare findings and fixes, and the differences
// fixture pins each fix withheld.
//
// It sits on the engine's syntax dispatcher, entering each file and hearing each call within it on the walk the
// dispatcher makes once for every syntax rule.
package string_int_conv

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
)

// passName is the original's name, which the port takes, so the rule, its settings and every `//cohere:ignore`
// naming it read the same as before the port took the original's place.
const passName = "stringintconv"

// passDoc is the original's documentation, word for word, so the rule is described in `go vet`'s own words.
const passDoc = "check for string(int) conversions\n\n" +
	"This checker flags conversions of the form string(x) where x is an integer\n" +
	"(but not byte or rune) type. Such conversions are discouraged because they\n" +
	"return the UTF-8 representation of the Unicode code point x, and not a decimal\n" +
	"string representation of x as one might expect. Furthermore, if x denotes an\n" +
	"invalid code point, the conversion cannot be statically rejected.\n\n" +
	"For conversions that intend on using the code point, consider replacing them\n" +
	"with string(rune(x)). Otherwise, strconv.Itoa and its equivalents return the\n" +
	"string representation of the value in the desired base."

// passUrl is the original's address.
const passUrl = "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stringintconv"

// Listener is what the port hears of each package from the dispatcher: each file as the walk enters it, and every
// call within it, in preorder, as the original's Preorder over files and calls visits them, of which it judges the
// conversions of an integer to a string.
var Listener = rule.Listener{
	FileEnter: enterFile,
	Nodes:     []ast.Node{(*ast.CallExpr)(nil)},
	Node:      checkCall,
}

// Analyzer runs the port under the original's name, documentation and address, reporting what the shared syntax
// dispatcher's walk found for Listener.
var Analyzer = newAnalyzer(dispatch.SharedSyntax())

// newAnalyzer registers Listener on dispatcher and gives the analyzer it returns the original's address, which
// RuleAnalyzer leaves unset.
func newAnalyzer(dispatcher *dispatch.Syntax) *analysis.Analyzer {
	analyzer := dispatcher.RuleAnalyzer(rule.Namespace+passName, passName, passDoc, &Listener)
	analyzer.URL = passUrl
	return analyzer
}

// walkState is what the port keeps over the package's walk, held in its context's state.
type walkState struct {
	// file is the file the walk is in, which the fix that imports fmt edits. The walk enters each file before the
	// calls within it, as the original's Preorder does.
	file *ast.File
}

// enterFile records file as the one the walk is in.
func enterFile(context *rule.Context, file *ast.File) {
	rule.State[walkState](context).file = file
}

// checkCall judges node, a call within the file the walk is in, as a conversion.
func checkCall(context *rule.Context, node ast.Node, _ []ast.Node) {
	call, isCall := node.(*ast.CallExpr)
	if !isCall {
		return
	}
	checkConversion(context, rule.State[walkState](context).file, call)
}

// checkConversion reports call, within file, when it converts an integer that is neither a byte nor a rune to a
// string, with the fixes that apply.
func checkConversion(context *rule.Context, file *ast.File, call *ast.CallExpr) {
	if len(call.Args) != 1 {
		return
	}
	// The type the call converts to, named as written: an identifier or a qualified one. A call of anything else,
	// a parenthesized type among them, is left alone, as the original leaves it.
	var converter types.Object
	switch named := call.Fun.(type) {
	case *ast.Ident:
		converter = context.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		converter = context.TypesInfo.Uses[named.Sel]
	}
	// A call of a function, a method or a value of function type is no conversion.
	target, isTypeName := converter.(*types.TypeName)
	if !isTypeName {
		return
	}

	// In the conversion T(v) of a value v of type V to a target type T, a type T0 in the type set of T and a type V0
	// in the type set of V make the conversion one of an integer to a string. When neither is a type parameter, each
	// set is the type alone.

	// First, the first type in T's set whose underlying type is a string.
	targetTypes, targetError := structuralTypes(target.Type())
	if targetError != nil {
		return // An invalid type, as the original reads one.
	}
	var targetString types.Type
	for _, candidate := range targetTypes {
		underlying, isBasic := candidate.Underlying().(*types.Basic)
		if isBasic && underlying.Kind() == types.String {
			targetString = candidate
			break
		}
	}
	if targetString == nil {
		return
	}

	// Next, the first type in V's set whose underlying type is an integer that is neither a byte nor a rune. types.Byte
	// and types.Rune are the kinds of uint8 and int32, so those are passed over too.
	argument := call.Args[0]
	sourceType := context.TypesInfo.TypeOf(argument)
	sourceTypes, sourceError := structuralTypes(sourceType)
	if sourceError != nil {
		return // An invalid type, as the original reads one.
	}
	var sourceInteger types.Type
	for _, candidate := range sourceTypes {
		underlying, isBasic := candidate.Underlying().(*types.Basic)
		if !isBasic || underlying.Info()&types.IsInteger == 0 {
			continue
		}
		if kind := underlying.Kind(); kind == types.Byte || kind == types.Rune || kind == types.UntypedRune {
			continue
		}
		sourceInteger = candidate
		break
	}
	if sourceInteger == nil {
		return
	}

	targetDescription := describe(targetString, target.Type(), target.Name())
	sourceDescription := describe(sourceInteger, sourceType, typeName(sourceType))
	if targetDescription == "" || sourceDescription == "" {
		return // A type with no name to print, which the original passes over too.
	}

	diagnostic := analysis.Diagnostic{
		Pos: call.Pos(),
		Message: fmt.Sprintf(
			"conversion from %s to %s yields a string of one rune, not a string of digits",
			sourceDescription, targetDescription,
		),
	}

	// Fix 1: format the number with fmt.Sprint, which works for any integer type where strconv.Itoa, FormatInt and
	// FormatUint each take one, importing fmt as the file needs. A target that is not exactly string keeps its
	// conversion around the formatted number. The original offers it only where each side's set holds one type, and
	// the source's type has no methods, since a String, GoString or Format method may change what fmt.Sprint prints.
	// The port withholds it in two more places, where it would be wrong: where `fmt.Sprint(x)` would replace a
	// conversion whose type is not string, and where the value's type, a type parameter whose constraint has a
	// Format, Error or String method, prints itself. A term of the constraint with such a method is passed over
	// already: it is the one term, sourceInteger, whose methods the original's check reads. Every other suggestion is
	// the original's.
	if len(targetTypes) == 1 && len(sourceTypes) == 1 && types.NewMethodSet(sourceInteger).Len() == 0 &&
		!printsItself(sourceType) {
		prefix, importEdits := addImport(context.TypesInfo, file, "fmt", "fmt", "Sprint", argument.Pos())
		switch {
		case !types.Identical(targetString, types.Typ[types.String]):
			// `mystring(x)` becomes `mystring(fmt.Sprint(x))`, which keeps the conversion and so its type.
			diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
				Message: "Format the number as a decimal",
				TextEdits: append(importEdits,
					analysis.TextEdit{Pos: call.Lparen + 1, End: call.Lparen + 1, NewText: []byte(prefix + "Sprint(")},
					analysis.TextEdit{Pos: call.Rparen, End: call.Rparen, NewText: []byte(")")},
				),
			})
		case types.Identical(context.TypesInfo.TypeOf(call), types.Typ[types.String]):
			// `string(x)` becomes `fmt.Sprint(x)`, a string as the conversion was. A conversion to a type parameter
			// whose one term is string, `AllString(x)`, is not one, so the original's fix there would change the
			// result's type, and the port does not offer it.
			diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
				Message: "Format the number as a decimal",
				TextEdits: append(importEdits, analysis.TextEdit{
					Pos:     call.Fun.Pos(),
					End:     call.Fun.End(),
					NewText: []byte(prefix + "Sprint"),
				}),
			})
		}
	}

	// Fix 2: convert the value to a rune first, `string(rune(x))`, where every type in V's set converts to a rune.
	if convertibleToRune(sourceTypes) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message: "Convert a single rune to a string",
			TextEdits: []analysis.TextEdit{
				{Pos: argument.Pos(), End: argument.Pos(), NewText: []byte("rune(")},
				{Pos: argument.End(), End: argument.End(), NewText: []byte(")")},
			},
		})
	}
	context.Report(diagnostic)
}

// convertibleToRune is whether every one of candidates converts to a rune.
func convertibleToRune(candidates []types.Type) bool {
	for _, candidate := range candidates {
		if !types.ConvertibleTo(candidate, types.Typ[types.Rune]) {
			return false
		}
	}
	return true
}

// structuralTypes is the types in candidate's type set: the type of each term of a type parameter's normalized
// constraint, none for a constraint every type satisfies, and candidate alone for any other type. It is an error
// where the constraint's terms cannot be computed or no type satisfies it. x/tools' stringintconv structuralTypes.
func structuralTypes(candidate types.Type) ([]types.Type, error) {
	parameter, isParameter := types.Unalias(candidate).(*types.TypeParam)
	if !isParameter {
		return []types.Type{candidate}, nil
	}
	terms, termsError := structuralTerms(parameter)
	if termsError != nil {
		return nil, termsError
	}
	var termTypes []types.Type
	for _, term := range terms {
		termTypes = append(termTypes, term.Type())
	}
	return termTypes, nil
}
