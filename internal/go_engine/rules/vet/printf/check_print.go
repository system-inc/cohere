package printf

import (
	"go/ast"
	"go/types"
	"regexp"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// The parts of printFormatPattern. The flags leave out the space, so printing "x % y" is not taken for a format.
const (
	flagsPattern          = `[+\-#]*`
	optionalIndexPattern  = `(\[[0-9]+\])?`
	optionalNumberPattern = `([0-9]+|` + optionalIndexPattern + `\*)?`
	verbPattern           = `[bcdefgopqstvxEFGTUX]`
)

// printFormatPattern matches what checkPrint reports as a possible printf directive in the first argument of a print
// function such as fmt.Print.
var printFormatPattern = regexp.MustCompile(`%` + flagsPattern + optionalNumberPattern + `\.?` +
	optionalNumberPattern + optionalIndexPattern + verbPattern)

// checkPrint checks a call of a print function such as fmt.Println, named name.
func checkPrint(pass *analysis.Pass, call *ast.CallExpr, name string) {
	calleeType := pass.TypesInfo.Types[call.Fun].Type
	if calleeType == nil {
		return // The callee's type is unknown.
	}
	firstArgument := 0
	if signature, isSignature := calleeType.Underlying().(*types.Signature); isSignature {
		if !signature.Variadic() {
			return
		}
		firstArgument = signature.Params().Len() - 1
		// A variadic parameter is a slice. One whose elements are not the empty interface takes no print arguments.
		variadic, isSlice := signature.Params().At(firstArgument).Type().(*types.Slice)
		if !isSlice {
			return
		}
		element, isInterface := types.Unalias(variadic.Elem()).(*types.Interface)
		if !isInterface || !element.Empty() {
			return
		}
	}
	// A call with no variadic arguments has nothing to check.
	if len(call.Args) <= firstArgument {
		return
	}
	arguments := call.Args[firstArgument:]
	if firstArgument == 0 && isStandardStream(call.Args[0]) {
		pass.ReportRangef(call, "%s does not take io.Writer but has first arg %s", name,
			formatNode(pass, call.Args[0]))
	}
	if text, isConstant := stringConstant(pass, arguments[0]); isConstant {
		// A trailing "%", as in "abc 0.0%", cannot be a directive.
		reportPossibleDirective(pass, call, name, strings.TrimSuffix(text, "%"))
	}
	for _, argument := range arguments {
		if isFunctionValue(pass, argument) {
			pass.ReportRangef(call, "%s arg %s is a func value, not called", name, formatNode(pass, argument))
		}
		if methodName, recursive := recursiveStringer(pass, argument); recursive {
			pass.ReportRangef(call, "%s arg %s causes recursive call to %s method", name, formatNode(pass, argument),
				methodName)
		}
	}
}

// isStandardStream reports whether expression is a selector of package-like name os whose selected name starts
// with Std, such as os.Stdout, passed to a print function as if it were Fprint.
func isStandardStream(expression ast.Expr) bool {
	selector, isSelector := expression.(*ast.SelectorExpr)
	if !isSelector {
		return false
	}
	packageName, isIdentifier := selector.X.(*ast.Ident)
	return isIdentifier && packageName.Name == "os" && strings.HasPrefix(selector.Sel.Name, "Std")
}

// reportPossibleDirective reports the first match of printFormatPattern in text, the constant first argument of a
// print call. A match like %XX of two hex digits is passed by, as common in URLs.
func reportPossibleDirective(pass *analysis.Pass, call *ast.CallExpr, name string, text string) {
	if !strings.Contains(text, "%") {
		return
	}
	for _, match := range printFormatPattern.FindAllString(text, -1) {
		if len(match) >= 3 && isHex(match[1]) && isHex(match[2]) {
			continue
		}
		pass.ReportRangef(call, "%s call has possible Printf formatting directive %s", name, match)
		return
	}
}

// isHex reports whether character is a hexadecimal digit.
func isHex(character byte) bool {
	return '0' <= character && character <= '9' || 'A' <= character && character <= 'F' ||
		'a' <= character && character <= 'f'
}
