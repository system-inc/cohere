package parity

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

func TestRequireStaticcheckWantsPassesAPortThatMeetsThem(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, port, analysistest.TestData(), "staticcheck_notes", "module_notes")
	}))
}

// A note nothing found fails, a pattern's as much as a text's, and so does a finding no note on its line expects.
func TestRequireStaticcheckWantsFailsEitherWay(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, original, analysistest.TestData(), "staticcheck_notes")
	}),
		`staticcheck_notes.go:15: the port reported "call to renamed in the original's words" in staticcheck_notes, `+
			"which no note on the line expects",
		`staticcheck_notes.go:14: diag(re"call to gai.ed"): the port reported nothing in staticcheck_notes matching it`,
		`staticcheck_notes.go:15: diag("in the port's words"): the port reported nothing in staticcheck_notes `+
			"matching it",
	)
}

// The package and its test variant are each held to the notes of their own files: the package finds one call and
// meets its one note, the test variant finds both and meets both. Held to every note of the folder at once, the
// package's finding and the test variant's on the shared line would be one finding too many for its note.
func TestRequireStaticcheckWantsHoldsEachPackageToItsOwnFiles(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, port, analysistest.TestData(), "staticcheck_tested")
	}))
}

// A fixture module loads under its go.mod, and its files are named after its pattern.
func TestRequireStaticcheckWantsLoadsAModule(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, original, analysistest.TestData(), "module_notes")
	}), `module_notes/module_notes.go:11: diag("call to gained"): the port reported nothing in `+
		"example.com/module_notes")
}

// Fixtures with no note prove nothing, even where the port reports nothing either.
func TestRequireStaticcheckWantsFailsFixturesWithNoNote(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckWants(recording, port, analysistest.TestData(), "silent")
	}), "the fixtures hold no note, so the comparison proves nothing")
}
