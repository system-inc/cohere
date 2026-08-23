package core

import (
	"testing"

	"github.com/system-inc/verify/internal/ruletest"
)

// callerFile is where the fixtures pretend to live.
const callerFile = "/repository/source/Caller.ts"

// The corpus is oxc's, copied rather than rewritten.
//
// Verbatim from `oxc/crates/oxc_linter/src/rules/eslint/no_caller.rs`: 4 pass, 2 fail, and the
// snapshot records 2 diagnostics from those 2 inputs, so one finding per input holds here rather
// than being assumed.
func TestNoCallerFires(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"callee", "var x = arguments.callee"},
		{"caller", "var x = arguments.caller"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectFindings(t,
				ruletest.Run(t, NoCaller, callerFile, testCase.sourceText), "noCaller")
		})
	}
}

// The fourth clean case is the one that matters and it is upstream's.
//
// `arguments[caller]` reads a variable named caller and accesses whatever property that names, so
// it is a different access from `arguments.caller` and may be neither of the two. A rule matching
// on source text reports it, and one treating a computed access as equivalent to a static member
// does too.
func TestNoCallerStaysSilent(t *testing.T) {
	cases := []struct {
		name       string
		sourceText string
	}{
		{"an ordinary property", "var x = arguments.length"},
		{"the object itself", "var x = arguments"},
		{"a numeric subscript", "var x = arguments[0]"},
		{"a subscript through a variable", "var x = arguments[caller]"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			ruletest.ExpectClean(t, ruletest.Run(t, NoCaller, callerFile, testCase.sourceText))
		})
	}
}

// Cases written because somebody read our code rather than upstream's.
//
// The imported corpus never writes the property on a receiver that is not `arguments`, and never
// parenthesizes the receiver. The first would report under a rule that checked only the property
// name; the second is the behavior `isIdentifierNamed` gives us for free by skipping parentheses,
// and upstream's `is_specific_id` does the same, so it is parity rather than divergence.
func TestNoCallerReadsTheReceiver(t *testing.T) {
	ruletest.ExpectClean(t, ruletest.Run(t, NoCaller, callerFile,
		"declare const options: { callee: number };\nexport const a = options.callee;\n"))

	ruletest.ExpectFindings(t, ruletest.Run(t, NoCaller, callerFile,
		"export function f() { return (arguments).callee; }\n"), "noCaller")
}
