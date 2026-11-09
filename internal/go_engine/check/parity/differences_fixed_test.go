package parity

import (
	"go/ast"
	"go/token"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// writingPast suggests replacing the call and widerBy bytes after it with text, as writing does with none.
func writingPast(text string, widerBy token.Pos) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message:   "write " + text,
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End() + widerBy, NewText: []byte(text)}},
		})
	}
}

// fixingMessages are the calls a planted fixed fixture's port finds: to shared, as the original does, and to gained.
var fixingMessages = map[string]string{"shared": "call to shared", "gained": "call to gained"}

// Every fix the port offers in a differences fixture's file, on the call it alone finds and on the one both find,
// applied, makes the fixed file beside it byte for byte, and passes.
func TestRequireOnlyAdjudicatedPassesFixesThatMakeTheFixedFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, writing("fixed()")),
			calling(fixingMessages, writing("fixed()")), analysistest.TestData(), "differences_fixed")
	}))
}

// A fixed file one byte off what the fixes make fails, naming the first line the two part on, each quoted so the
// byte shows.
func TestRequireOnlyAdjudicatedFailsAFixedFileOneByteOff(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, writing("fixed()")),
			calling(fixingMessages, writing("fixed()")), analysistest.TestData(), "differences_fixed_off")
	}), "the port's fixes in differences_fixed_off/differences_fixed_off.go, applied, do not make "+
		`differences_fixed_off/differences_fixed_off.go.fixed: on line 13 they leave "\tfixed()\n", and it holds `+
		`"\tfixeD()\n"`)
}

// A fix one byte wider than the call, eating the byte after it, still starts on its finding's line and writes the
// recorded text, so the `fixes:` record is met, as over fixed, which has no fixed file. Here it eats the space before
// the found call's comment and the newline before the closing brace, which gofmt puts back, so the same fixture's
// golden file, held formatted by RequireStaticcheckGolden, passes it, and the fixed file fails it, on the first line.
func TestRequireOnlyAdjudicatedFailsAFixOneByteWiderThanTheFixedFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages),
			calling(gainedMessages, writingPast("fixed()", 1)), analysistest.TestData(), "fixed")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, calling(fixingMessages, writingPast("fixed()", 1)),
			analysistest.TestData(), "differences_fixed")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, writingPast("fixed()", 1)),
			calling(fixingMessages, writingPast("fixed()", 1)), analysistest.TestData(), "differences_fixed")
	}), "the port's fixes in differences_fixed/differences_fixed.go, applied, do not make "+
		`differences_fixed/differences_fixed.go.fixed: on line 13 they leave "\tfixed()// port finds: call to `+
		`gained // port fixes: fixed()\n", and it holds "\tfixed() // port finds: call to gained // port fixes: `+
		`fixed()\n"`)
}

// Where the original panics, the port's fixes are held to the fixed file as in RequireOnlyAdjudicated: making it,
// they pass, and one byte wider, eating the space before a comment, they fail.
func TestRequirePortWhereOriginalPanicsHoldsFixesToTheFixedFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages),
			calling(gainedMessages, writing("fixed()")), analysistest.TestData(), "panicking_fixed")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequirePortWhereOriginalPanics(recording, panicking(originalMessages),
			calling(gainedMessages, writingPast("fixed()", 1)), analysistest.TestData(), "panicking_fixed")
	}), "the port's fixes in panicking_fixed/panicking_fixed.go, applied, do not make "+
		`panicking_fixed/panicking_fixed.go.fixed: on line 13 they leave "\tfixed()// port finds: call to gained `+
		`// port fixes: fixed()\n", and it holds "\tfixed() // port finds: call to gained // port fixes: fixed()\n"`)
}
