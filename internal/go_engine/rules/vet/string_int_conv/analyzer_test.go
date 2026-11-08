package string_int_conv

import (
	"testing"

	"golang.org/x/tools/go/analysis/passes/stringintconv"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// x/tools' own fixtures for stringintconv, copied unchanged into testdata/src with their golden files, hold the port
// to every want the original expects, and to nothing more: a and typeparams for their wants, and fix with its
// suggested fixes too, every fix the port offers there, applied, turning each file into its golden one, as x/tools'
// test runs it.
func TestPortMatchesUpstreamWants(test *testing.T) {
	test.Parallel()
	parity.RequireUpstreamWants(test, Analyzer, analysistest.TestData(), "a", "typeparams")
	parity.RequireUpstreamWantsWithFixes(test, Analyzer, analysistest.TestData(), "fix")
}

// Over x/tools' fixtures and parity's, the second holding every branch of the judgment and of both fixes, the import
// each fix adds in every layout of a file's imports among them, and the shapes most likely to differ, the original
// and the port find the same findings, each the same in every field, every fix edit for edit, which a golden file,
// gofmt'd before it is compared, would not hold to the character. x/tools' typeparams holds one fix the port
// withholds, so it is held to the original in TestPortWithholdsOnlyTheRecordedFixInUpstreamTypeparams.
func TestPortMatchesOriginalFindingForFinding(test *testing.T) {
	test.Parallel()
	parity.RequireIdentical(test, stringintconv.Analyzer, Analyzer, analysistest.TestData(),
		"a", "fix", "parity", "parity/fresh", "example.com/library")
}

// Over the differences fixture the two make the same findings, and offer the same fixes, edit for edit, but where a
// record says the port withholds the original's "Format the number as a decimal", which would be wrong applied, each
// record with its reason on the line above it.
func TestPortDiffersFromOriginalOnlyWhereItIsRight(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, stringintconv.Analyzer, Analyzer, analysistest.TestData(), "differences")
}

// Over x/tools' typeparams the two make the same findings, each the same in every field, but where the port withholds
// the original's "Format the number as a decimal": `AllString(p)`, where fmt.Sprint(p) is a string and the conversion
// it would replace is an AllString, a type parameter whose one term is string, so applied it would change the type.
// That file, copied byte for byte, cannot carry a record, so the record is given beside the call with its reason.
func TestPortWithholdsOnlyTheRecordedFixInUpstreamTypeparams(test *testing.T) {
	test.Parallel()
	parity.RequireOnlyAdjudicated(test, stringintconv.Analyzer, Analyzer, analysistest.TestData(), "typeparams",
		parity.Record{
			File: "typeparams/typeparams.go",
			Line: 34,
			Text: "withholds: Format the number as a decimal",
			Reason: "fmt.Sprint(p) is a string, where the conversion it would replace is an AllString, a type " +
				"parameter whose one term is string, so the decimal fix would change the type",
		},
	)
}
