// Package string_int_conv is vet's stringintconv pass, a conversion `string(x)` of an integer x that is neither a
// byte nor a rune, which yields the UTF-8 encoding of the code point x, a string of one rune, where the writer most
// likely meant x's decimal digits. A conversion to a type parameter, or of a value of one, is judged over the type
// sets: it is reported when some type in the target's set is a string and some type in the source's set is such an
// integer. A finding offers to format the number with fmt.Sprint, importing fmt where the file needs it, when neither
// side is a type parameter and the source's type has no methods, and to convert the value to a rune first, when every
// type in the source's set converts to one.
//
// It is translated, not copied, from golang.org/x/tools@v0.50.0 go/analysis/passes/stringintconv/string.go, with the
// helpers it calls from x/tools' internal packages (typeparams.StructuralTerms, typesinternal.TypeNameFor and
// refactor.AddImport) translated beside it, and it reports the original's messages word for word, at the same
// position, with the same suggested fixes, edit for edit. The registry runs the port in the original's place, and the
// original stays the oracle: the parity tests beside this file run both over the same packages and compare findings
// and fixes. The two agree everywhere, so there is no differences fixture.
package string_int_conv

import (
	"fmt"
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer runs the port under the original's name, documentation and address, so the rule, its settings and every
// `//cohere:ignore` naming it read the same as before the port took the original's place.
var Analyzer = &analysis.Analyzer{
	Name: "stringintconv",
	Doc: "check for string(int) conversions\n\n" +
		"This checker flags conversions of the form string(x) where x is an integer\n" +
		"(but not byte or rune) type. Such conversions are discouraged because they\n" +
		"return the UTF-8 representation of the Unicode code point x, and not a decimal\n" +
		"string representation of x as one might expect. Furthermore, if x denotes an\n" +
		"invalid code point, the conversion cannot be statically rejected.\n\n" +
		"For conversions that intend on using the code point, consider replacing them\n" +
		"with string(rune(x)). Otherwise, strconv.Itoa and its equivalents return the\n" +
		"string representation of the value in the desired base.",
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stringintconv",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	inspected, isInspector := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	if !isInspector {
		// Invariant: the driver hands every analyzer the results of what it requires, of the types they declare.
		panic("string_int_conv: inspect's result is missing")
	}
	// The file the walk is in, which the fix that imports fmt edits. Preorder visits each file before the calls within
	// it, as the original's does, and its type switch hands each node over typed, with no assertion to check.
	var file *ast.File
	inspected.Preorder([]ast.Node{(*ast.File)(nil), (*ast.CallExpr)(nil)}, func(node ast.Node) {
		switch visited := node.(type) {
		case *ast.File:
			file = visited
		case *ast.CallExpr:
			checkConversion(pass, file, visited)
		}
	})
	return nil, nil
}

// checkConversion reports call, within file, when it converts an integer that is neither a byte nor a rune to a
// string, with the fixes that apply.
func checkConversion(pass *analysis.Pass, file *ast.File, call *ast.CallExpr) {
	if len(call.Args) != 1 {
		return
	}
	// The type the call converts to, named as written: an identifier or a qualified one. A call of anything else,
	// a parenthesized type among them, is left alone, as the original leaves it.
	var converter types.Object
	switch named := call.Fun.(type) {
	case *ast.Ident:
		converter = pass.TypesInfo.Uses[named]
	case *ast.SelectorExpr:
		converter = pass.TypesInfo.Uses[named.Sel]
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
	sourceType := pass.TypesInfo.TypeOf(argument)
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
	// conversion around the formatted number. The fix is not offered where a type parameter is involved, whose many
	// combinations differ subtly in the finding and the fix they want, nor for a source type with methods, since a
	// String, GoString or Format method may change what fmt.Sprint prints.
	if len(targetTypes) == 1 && len(sourceTypes) == 1 && types.NewMethodSet(sourceInteger).Len() == 0 {
		prefix, importEdits := addImport(pass.TypesInfo, file, "fmt", "fmt", "Sprint", argument.Pos())
		if types.Identical(targetString, types.Typ[types.String]) {
			// `string(x)` becomes `fmt.Sprint(x)`.
			diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
				Message: "Format the number as a decimal",
				TextEdits: append(importEdits, analysis.TextEdit{
					Pos:     call.Fun.Pos(),
					End:     call.Fun.End(),
					NewText: []byte(prefix + "Sprint"),
				}),
			})
		} else {
			// `mystring(x)` becomes `mystring(fmt.Sprint(x))`.
			diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
				Message: "Format the number as a decimal",
				TextEdits: append(importEdits,
					analysis.TextEdit{Pos: call.Lparen + 1, End: call.Lparen + 1, NewText: []byte(prefix + "Sprint(")},
					analysis.TextEdit{Pos: call.Rparen, End: call.Rparen, NewText: []byte(")")},
				),
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
	pass.Report(diagnostic)
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
