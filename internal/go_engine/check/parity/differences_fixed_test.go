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
// the found call's comment and the newline before the closing brace, which gofmt puts back, so the golden file of the
// same source, differences_golden's, held formatted by RequireStaticcheckGolden, passes it, and the fixed file fails
// it, on the first line.
func TestRequireOnlyAdjudicatedFailsAFixOneByteWiderThanTheFixedFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages),
			calling(gainedMessages, writingPast("fixed()", 1)), analysistest.TestData(), "fixed")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, calling(fixingMessages, writingPast("fixed()", 1)),
			analysistest.TestData(), "differences_golden")
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

// In a file with a fixed file, a move must prove the original's fix wrong: the original's fixes on its moved finding,
// applied in place of the port's, must not make the fixed file. The two here differ in span, and the port fixes the
// call by writing fixed(). The original writing other() passes, and the original writing fixed() too, whose fix is
// right, fails, named by its finding.
func TestRequireOnlyAdjudicatedFailsAMoveWhoseOriginalFixMakesTheFixedFile(test *testing.T) {
	test.Parallel()
	port := calling(sharedMessages, endingShort(1), writing("fixed()"))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, endingShort(0), writing("other()")), port,
			analysistest.TestData(), "moved_fixed")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, endingShort(0), writing("fixed()")), port,
			analysistest.TestData(), "moved_fixed")
	}), "the original's fixes on its moved finding at moved_fixed/moved_fixed.go:10, applied in place of the port's, "+
		"make moved_fixed/moved_fixed.go.fixed too, so the move proves no fix of the original's wrong")
}

// A fixed file whose source is not beside it, as one left behind when its source was renamed, is read by nothing, and
// so is one beside a file that is there but holds no records, notes.txt, so each fails, named, though every record of
// the fixture is met.
func TestRequireOnlyAdjudicatedFailsAFixedFileWithNoSource(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireOnlyAdjudicated(recording, calling(sharedMessages, writing("fixed()")),
			calling(fixingMessages, writing("fixed()")), analysistest.TestData(), "fixed_orphan")
	}),
		"fixed_orphan/notes.txt.fixed is a fixed file with no source beside it that holds records, notes.txt, so "+
			"nothing reads it: rename it for its source, or delete it",
		"fixed_orphan/renamed.go.fixed is a fixed file with no source beside it that holds records, renamed.go, so "+
			"nothing reads it: rename it for its source, or delete it",
	)
}

// A fixed file beside a fixture package that a form holding no fixed file loads is read by nothing, so it fails, named,
// where the same fixture's other files load as before.
func TestAFormHoldingNoFixedFileFailsOneBesideItsFixture(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		Findings(recording, calling(sharedMessages), calling(sharedMessages), analysistest.TestData(),
			"differences_fixed")
	}), "differences_fixed/differences_fixed.go.fixed is a fixed file beside a fixture package this call loads, and "+
		"it holds no fixed file, so nothing reads it: hold it with RequireOnlyAdjudicated or "+
		"RequirePortWhereOriginalPanics, or delete it")
}
