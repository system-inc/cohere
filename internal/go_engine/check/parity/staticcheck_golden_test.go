package parity

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
)

// replacing suggests replacing the call with replacement, in a fix with message.
func replacing(message string, replacement string) shape {
	return func(call *ast.CallExpr, diagnostic *analysis.Diagnostic) {
		diagnostic.SuggestedFixes = append(diagnostic.SuggestedFixes, analysis.SuggestedFix{
			Message:   message,
			TextEdits: []analysis.TextEdit{{Pos: call.Pos(), End: call.End(), NewText: []byte(replacement)}},
		})
	}
}

// keeping is a port whose one fix replaces each call to shared with a call to kept, as every golden file of the
// fixtures but golden_sections' expects.
var keeping = calling(sharedMessages, replacing("replace with kept", "kept()"))

// A golden file with no sections is the file with every fix applied, and one with sections holds each fix's own.
func TestRequireStaticcheckGoldenPassesFixesThatMakeTheGoldenFiles(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "golden_whole")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, calling(sharedMessages, replacing("replace with kept", "kept()"),
			replacing("replace with other", "other()")), analysistest.TestData(), "golden_sections")
	}))
}

func TestRequireStaticcheckGoldenFailsAnEditWhoseTextDiffers(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, calling(sharedMessages, replacing("replace with kept", "other()")),
			analysistest.TestData(), "golden_whole")
	}), "where golden_whole.go.golden holds")
}

// A golden file no fix reaches is held all the same, so a port that offers none where the golden file holds one
// fails.
func TestRequireStaticcheckGoldenFailsAGoldenFileNoFixReaches(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, port, analysistest.TestData(), "golden_whole")
	}), "the port's fixes in golden_whole make\n// Package golden_whole")
}

// A fix the golden file holds no section for fails, and so does a section no fix is named for.
func TestRequireStaticcheckGoldenFailsASectionEitherWay(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, calling(sharedMessages, replacing("replace with kept", "kept()"),
			replacing("replace with third", "kept()")), analysistest.TestData(), "golden_sections")
	}),
		`golden_sections.go.golden holds no section for the fix "replace with third" the port suggests in `+
			"golden_sections",
		`golden_sections.go.golden holds a section for the fix "replace with other", which the port never suggests `+
			"in golden_sections",
	)
}

func TestRequireStaticcheckGoldenRefusesSectionsBesideAComment(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "golden_commented")
	}), "golden_commented.go.golden holds sections and a comment beside them, which staticcheck's testutil refuses")
}

// A fix in a file with no golden file fails, and fixtures with no golden file prove nothing.
func TestRequireStaticcheckGoldenFailsAFixWithNoGoldenFile(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "same")
	}),
		"same.go: the port's fixes in same edit it, and it has no golden file",
		"the fixtures hold no golden file, so the comparison proves nothing",
	)
}

// The package and its test variant are each held to their own fixes: the shared file's golden file takes the
// package's one edit there, and the test variant's one edit there, never both at once, which would overlap.
func TestRequireStaticcheckGoldenHoldsEachPackageApart(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "golden_tested")
	}))
}

// A fixture module loads under its go.mod, and its golden file is named after its pattern.
func TestRequireStaticcheckGoldenLoadsAModule(test *testing.T) {
	test.Parallel()
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, keeping, analysistest.TestData(), "module_golden")
	}))
	requireFailures(test, failuresOf(test, func(recording testing.TB) {
		RequireStaticcheckGolden(recording, port, analysistest.TestData(), "module_golden")
	}), "where module_golden/module_golden.go.golden holds")
}
