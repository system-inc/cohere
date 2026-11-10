package printf

import (
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// printVerb is a verb printf knows, the flags it takes and the kinds of operand it accepts.
type printVerb struct {
	// verb is a rune, since a Formatter may take any.
	verb rune

	// flags are all ASCII.
	flags string

	accepted argumentType
}

// The common flag sets of printf's verbs.
const (
	noFlag           = ""
	numberFlags      = " -+.0"
	sharpNumberFlags = " -+.0#"
	allFlags         = " -+.0#"
)

// hexadecimalArguments are what %x and %X accept.
const hexadecimalArguments = argumentRune | argumentInt | argumentString | argumentPointer | argumentFloat |
	argumentComplex

// printVerbs are the verbs printf knows. A `-` is a width modifier, always valid; a `.` is a float's precision and a
// string's maximum width; a `+` is a number's sign and Go syntax for %v; a `#` is an alternate format for several
// verbs; a space pads numbers.
var printVerbs = []printVerb{
	{'%', noFlag, 0},
	{'b', sharpNumberFlags, argumentInt | argumentFloat | argumentComplex | argumentPointer},
	{'c', "-", argumentRune | argumentInt},
	// Code of go1.27 or later takes no pointer with %d (checkOperation).
	{'d', numberFlags, argumentInt | argumentPointer},
	{'e', sharpNumberFlags, argumentFloat | argumentComplex},
	{'E', sharpNumberFlags, argumentFloat | argumentComplex},
	{'f', sharpNumberFlags, argumentFloat | argumentComplex},
	{'F', sharpNumberFlags, argumentFloat | argumentComplex},
	{'g', sharpNumberFlags, argumentFloat | argumentComplex},
	{'G', sharpNumberFlags, argumentFloat | argumentComplex},
	{'o', sharpNumberFlags, argumentInt | argumentPointer},
	{'O', sharpNumberFlags, argumentInt | argumentPointer},
	{'p', "-#", argumentPointer},
	// Code of go1.26 or later takes only a byte or a rune of the integers with %q (checkOperation).
	{'q', " -+.0#", argumentRune | argumentInt | argumentString},
	{'s', " -+.0", argumentString},
	{'t', "-", argumentBool},
	{'T', "-", argumentAny},
	{'U', "-#", argumentRune | argumentInt},
	{'v', allFlags, argumentAny},
	{'w', allFlags, argumentError},
	{'x', sharpNumberFlags, hexadecimalArguments},
	{'X', sharpNumberFlags, hexadecimalArguments},
}

// checkPrintf checks a call of a printf function of kind, named name, in a file of fileVersion.
func checkPrintf(pass *analysis.Pass, fileVersion string, kind wrapperKind, call *ast.CallExpr, name string) {
	formatIndex := formatStringIndex(pass, call)
	if formatIndex < 0 || formatIndex >= len(call.Args) {
		return
	}
	formatArgument := call.Args[formatIndex]
	format, isConstant := stringConstant(pass, formatArgument)
	if !isConstant {
		// fmt.Printf(message) with no other arguments misformats a message that holds a "%": reported, with the
		// fix fmt.Printf("%s", message). It is gated on the file's language version, since it finds much in
		// existing code that is unlikely or not worth fixing (golang/go#71485), and an unknown version fails open.
		if formatIndex == len(call.Args)-1 && fileVersion != "" && atLeast(fileVersion, go124) {
			pass.Report(analysis.Diagnostic{
				Pos:     formatArgument.Pos(),
				End:     formatArgument.End(),
				Message: fmt.Sprintf("non-constant format string in call to %s", name),
				SuggestedFixes: []analysis.SuggestedFix{{
					Message: `Insert "%s" format string`,
					TextEdits: []analysis.TextEdit{{
						Pos:     formatArgument.Pos(),
						End:     formatArgument.Pos(),
						NewText: []byte(`"%s", `),
					}},
				}},
			})
		}
		return
	}
	check := &printfCall{
		pass:                 pass,
		call:                 call,
		name:                 name,
		fileVersion:          fileVersion,
		firstArgument:        formatIndex + 1,
		maximumArgumentIndex: formatIndex,
	}
	if !strings.Contains(format, "%") {
		if len(call.Args) > check.firstArgument {
			pass.ReportRangef(call.Args[check.firstArgument], "%s call has arguments but no formatting directives",
				name)
		}
		return
	}
	// The constant's value, so fmt.Sprintf("%"+("s"), "hi", 3) reports "fmt.Sprintf call needs 1 arg but has 2 args".
	operations, parseError := parseFormat(format, formatIndex)
	if parseError != nil {
		// Every parse error is in predicate form, so it reads after the function's name.
		pass.ReportRangef(formatArgument, "%s %s", name, parseError)
		return
	}
	anyIndex := false
	for _, operation := range operations {
		if operation.precision.index != -1 || operation.width.index != -1 || operation.verb.index != -1 {
			anyIndex = true
		}
		span := operationRange(formatArgument, operation)
		if operation.verb.verb == 'w' && kind != kindErrorf {
			pass.ReportRangef(span, "%s does not support error-wrapping directive %%w", name)
			return
		}
		// One finding for a format is enough.
		if !check.checkOperation(span, operation) {
			return
		}
	}
	// An argument list passed with `...` hides how many there are.
	if call.Ellipsis.IsValid() && check.maximumArgumentIndex >= len(call.Args)-2 {
		return
	}
	// With an indexed operation, extra arguments are ignored.
	if anyIndex {
		return
	}
	if check.maximumArgumentIndex+1 < len(call.Args) {
		expected := check.maximumArgumentIndex + 1 - check.firstArgument
		given := len(call.Args) - check.firstArgument
		pass.ReportRangef(call, "%s call needs %v but has %v", name, count(expected, "arg"), count(given, "arg"))
	}
}

// operationRange is where a finding about operation is reported: its text, such as the %v of "...%v...", when the
// format is a literal it can be placed in, and the whole format argument otherwise.
func operationRange(formatArgument ast.Expr, operation *formatOperation) analysis.Range {
	if literal, isLiteral := formatArgument.(*ast.BasicLit); isLiteral {
		span, spanError := rangeInStringLiteral(literal, operation.byteRange.start, operation.byteRange.end)
		if spanError == nil {
			return span
		}
	}
	return formatArgument
}

// formatStringIndex is the index of a printf call's format string, its last parameter before the variadic one, or
// -1 when the callee's type is unknown, not a function's, not variadic or has no parameter before the variadic one.
func formatStringIndex(pass *analysis.Pass, call *ast.CallExpr) int {
	calleeType := pass.TypesInfo.Types[call.Fun].Type
	if calleeType == nil {
		return -1
	}
	// The type itself, not its underlying type: a call through a named function type is not checked, as the
	// original does not check it.
	signature, isSignature := calleeType.(*types.Signature)
	if !isSignature || !signature.Variadic() {
		return -1
	}
	index := signature.Params().Len() - 2
	if index < 0 {
		return -1
	}
	return index
}

// stringConstant is expression's value when it is a string constant, and false otherwise.
func stringConstant(pass *analysis.Pass, expression ast.Expr) (string, bool) {
	value := pass.TypesInfo.Types[expression].Value
	if value != nil && value.Kind() == constant.String {
		return constant.StringVal(value), true
	}
	return "", false
}

// count is "1 what" or "N whats".
func count(number int, what string) string {
	if number == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", number, what)
}
