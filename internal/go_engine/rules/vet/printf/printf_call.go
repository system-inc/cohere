package printf

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/type_predicates/verbs"
)

// printfCall is one call of a printf function as checkPrintf checks its operations in turn.
type printfCall struct {
	pass *analysis.Pass
	call *ast.CallExpr

	// name is the called function's full name, the subject of every message.
	name string

	// fileVersion is the language version of the call's file, "" when unknown.
	fileVersion string

	// firstArgument is the index of the first argument after the format string.
	firstArgument int

	// maximumArgumentIndex is the highest argument index an operation has read so far.
	maximumArgumentIndex int
}

// lookUpVerb is the verb printf knows as verb, and false for one it does not. For an unknown verb it is the table's
// last entry, as the original's scan leaves it, which is read only as a verb that is not %w's.
func lookUpVerb(verb rune) (printVerb, bool) {
	for _, known := range printVerbs {
		if known.verb == verb {
			return known, true
		}
	}
	return printVerbs[len(printVerbs)-1], false
}

// checkOperation compares operation, reported at span, with the call's arguments and reports the first discrepancy
// it sees, and reports whether there was none: x/tools' printf okPrintfArg. It can do little with an argument list
// passed with `...`.
func (check *printfCall) checkOperation(span analysis.Range, operation *formatOperation) bool {
	verb := operation.verb.verb
	known, found := lookUpVerb(verb)
	// Code of go1.26 or later takes only a byte or a rune of the integers with %q (#72850). An unknown version fails
	// open.
	if verb == 'q' && check.fileVersion != "" && atLeast(check.fileVersion, go126) {
		known.accepted = verbs.Rune | verbs.Byte | verbs.String
	}
	// Code of go1.27 or later takes no pointer with %d (#62595).
	if verb == 'd' && check.fileVersion != "" && atLeast(check.fileVersion, go127) {
		known.accepted = verbs.Int
	}
	// An argument that may implement fmt.Formatter takes any verb and flag. %w's needs an error, so it is not asked.
	formatter := false
	if known.accepted != verbs.Error && operation.verb.argumentIndex < len(check.call.Args) {
		if typed, recorded := check.pass.TypesInfo.Types[check.call.Args[operation.verb.argumentIndex]]; recorded {
			formatter = verbs.IsFormatter(typed.Type)
		}
	}
	if !formatter && !check.flagsAreKnown(span, operation, known, found) {
		return false
	}
	var argumentIndexes []int
	// A `*` width or precision first, as in %.*s: each argument it reads must be an integer.
	if operation.width.dynamic != -1 {
		argumentIndexes = append(argumentIndexes, operation.width.dynamic)
	}
	if operation.precision.dynamic != -1 {
		argumentIndexes = append(argumentIndexes, operation.precision.dynamic)
	}
	for _, argumentIndex := range argumentIndexes {
		if !check.argumentCanBeChecked(span, argumentIndex, operation) {
			return false
		}
		argument := check.call.Args[argumentIndex]
		if reason, matched := matchArgumentType(check.pass, verbs.Int, argument); !matched {
			check.pass.ReportRangef(span, "%s format %s uses non-int %s%s as argument of *", check.name, operation.text,
				formatNode(check.pass, argument), detailsOf(reason))
			return false
		}
	}
	if operation.verb.argumentIndex != -1 && verb != '%' {
		argumentIndexes = append(argumentIndexes, operation.verb.argumentIndex)
	}
	for _, argumentIndex := range argumentIndexes {
		check.maximumArgumentIndex = max(check.maximumArgumentIndex, argumentIndex)
	}
	// fmt prints fmt.Printf("%10.2%%dhello", 4) as "%4hello": everything between the two percent signs is dropped
	// and the `%` verb is an ordinary rune, so it has no type to check, nor has a formatter's argument.
	if verb == '%' || formatter {
		return true
	}
	if !check.argumentCanBeChecked(span, operation.verb.argumentIndex, operation) {
		return false
	}
	return check.argumentFits(span, operation, known, check.call.Args[operation.verb.argumentIndex])
}

// flagsAreKnown reports an unknown verb or the first flag it does not take, and reports whether there was neither.
// The `0` flag is never reported (issues 23598 and 23605).
func (check *printfCall) flagsAreKnown(
	span analysis.Range,
	operation *formatOperation,
	known printVerb,
	found bool,
) bool {
	if !found {
		check.pass.ReportRangef(span, "%s format %s has unknown verb %c", check.name, operation.text,
			operation.verb.verb)
		return false
	}
	for _, flag := range operation.flags {
		if flag == '0' {
			continue
		}
		if !strings.ContainsRune(known.flags, flag) {
			check.pass.ReportRangef(span, "%s format %s has unrecognized flag %c", check.name, operation.text, flag)
			return false
		}
	}
	return true
}

// argumentFits reports the first way argument does not fit the verb known: a %w operand that is a pointer to an
// error, a function value not called, an operand of the wrong type, or a value whose String or Error method would
// call itself, and reports whether there was none.
func (check *printfCall) argumentFits(
	span analysis.Range,
	operation *formatOperation,
	known printVerb,
	argument ast.Expr,
) bool {
	verb := operation.verb.verb
	if verb == 'w' && check.wrapsPointerToError(span, argument) {
		return false
	}
	if isFunctionValue(check.pass, argument) && verb != 'p' && verb != 'T' {
		check.pass.ReportRangef(span, "%s format %s arg %s is a func value, not called", check.name, operation.text,
			formatNode(check.pass, argument))
		return false
	}
	if reason, matched := matchArgumentType(check.pass, known.accepted, argument); !matched {
		typeString := ""
		if passedType := check.pass.TypesInfo.Types[argument].Type; passedType != nil {
			typeString = passedType.String()
		}
		check.pass.ReportRangef(span, "%s format %s has arg %s of wrong type %s%s", check.name, operation.text,
			formatNode(check.pass, argument), typeString, detailsOf(reason))
		return false
	}
	// A value's String or Error method that formats the value calls itself. The `#` flag keeps fmt from calling
	// them, except with %x, %X and %q.
	if known.accepted&verbs.String != 0 && known.verb != 'T' &&
		(!strings.Contains(operation.flags, "#") || strings.ContainsRune("qxX", known.verb)) {
		if methodName, recursive := recursiveStringer(check.pass, argument); recursive {
			check.pass.ReportRangef(span, "%s format %s with arg %s causes recursive %s method call", check.name,
				operation.text, formatNode(check.pass, argument), methodName)
			return false
		}
	}
	return true
}

// wrapsPointerToError reports a %w operand of type *E where E implements error, in code of go1.27 or later, and
// reports whether it did. errors.Is and errors.As then behave unexpectedly with the error fmt.Errorf returns, when
// the pointer implements error too (golang/go#61342).
func (check *printfCall) wrapsPointerToError(span analysis.Range, argument ast.Expr) bool {
	operandType := check.pass.TypesInfo.Types[argument].Type
	if operandType == nil {
		return false
	}
	pointer, isPointer := operandType.Underlying().(*types.Pointer)
	if !isPointer || !types.Implements(pointer.Elem(), verbs.ErrorInterface) || !atLeast(check.fileVersion, go127) {
		return false
	}
	// A type of the package being checked is named without its package.
	qualifier := func(qualified *types.Package) string {
		if qualified == check.pass.Pkg {
			return ""
		}
		return qualified.Name()
	}
	check.pass.ReportRangef(span, "%%w wants operand of error type %s, not pointer type %s (defeats errors.Is)",
		types.TypeString(pointer.Elem(), qualifier), types.TypeString(operandType, qualifier))
	return true
}

// argumentCanBeChecked reports whether the argument at argumentIndex is in the call to be seen, not past its last
// argument or hidden in a final argument list passed with `...`, and reports a format that reads past the last.
func (check *printfCall) argumentCanBeChecked(
	span analysis.Range,
	argumentIndex int,
	operation *formatOperation,
) bool {
	if argumentIndex <= 0 {
		// Invariant: the parser numbers every argument an operation reads after the format string, which is at 0
		// or later.
		panic("an operation reads an argument before the format string")
	}
	if argumentIndex < len(check.call.Args)-1 {
		return true
	}
	if check.call.Ellipsis.IsValid() {
		return false // There could be any number of arguments more.
	}
	if argumentIndex < len(check.call.Args) {
		return true
	}
	// The format holds a bad index or reads more arguments than the call has. People count the arguments after the
	// format from 1: Printf("%s", "hi") reads arg #1.
	check.pass.ReportRangef(span, "%s format %s reads arg #%d, but call has %v", check.name, operation.text,
		argumentIndex-check.firstArgument+1, count(len(check.call.Args)-check.firstArgument, "arg"))
	return false
}

// detailsOf is a mismatch's reason as a message appends it, in parentheses after a space, and nothing without one.
func detailsOf(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}
