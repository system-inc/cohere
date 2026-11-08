// Package invalid_regular_expression is staticcheck's SA1000, a regular expression given as a constant to the regexp
// package that does not compile, ported onto staticcheck_call, the house's translation of the callcheck framework the
// original rides.
//
// It is translated, not copied, from honnef.co/go/tools@v0.8.1 staticcheck/sa1000/sa1000.go, whose rules it keeps
// and whose messages, the regexp package's own error, it reports word for word over the same range, the pattern
// argument's expression. The original stays the oracle: the parity tests beside this file run both over staticcheck's
// own fixture and the house's. The two differ nowhere: staticcheck_call departs from callcheck only on calls the
// regexp functions are never part of, as testdata/src/differences records.
package invalid_regular_expression

import (
	"go/constant"
	"regexp"

	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_call"
	"github.com/system-inc/cohere/internal/go_engine/rules/staticcheck_ir"
)

// Check judges every call in a function to a regexp function that compiles its first argument.
var Check = staticcheck_call.FunctionCheck(map[string]staticcheck_call.Rule{
	"regexp.MustCompile": checkPattern,
	"regexp.Compile":     checkPattern,
	"regexp.Match":       checkPattern,
	"regexp.MatchReader": checkPattern,
	"regexp.MatchString": checkPattern,
})

// Analyzer runs the port as a plain analyzer under the original's name, so the rule, its settings and every
// `//cohere:ignore` naming it read the same once the port takes the original's place.
var Analyzer = staticcheck_ir.FunctionCheckAnalyzer(
	"SA1000",
	"Flags a regular expression given as a constant that does not compile: staticcheck's SA1000, ported onto "+
		"staticcheck's intermediate representation.",
	Check,
)

// checkPattern marks the call's pattern, its first argument, with the error compiling it gives, when the pattern is
// a constant string. A pattern computed while the program runs is never judged.
func checkPattern(call *staticcheck_call.Call) {
	pattern := call.Arguments[0]
	extracted := staticcheck_call.ExtractConstantOfKind(pattern.Value, constant.String)
	if extracted == nil {
		return
	}
	if _, compileError := regexp.Compile(constant.StringVal(extracted.Value)); compileError != nil {
		pattern.Invalid(compileError.Error())
	}
}
