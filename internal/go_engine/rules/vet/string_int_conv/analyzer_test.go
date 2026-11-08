package string_int_conv

import (
	"slices"
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

// upstreamWithholding is a fix the port withholds in x/tools' own fixtures, by the file and line of the finding that
// offers it and the fix's message, which those files, copied byte for byte, cannot carry as a record. The file is
// named by its path below testdata's src, as the harness names it.
type upstreamWithholding struct {
	file    string
	line    int
	message string
}

// upstreamWithholdings is every fix the port withholds in x/tools' typeparams.
var upstreamWithholdings = []upstreamWithholding{
	// `AllString(p)`: fmt.Sprint(p) is a string, where the conversion it would replace is an AllString, a type
	// parameter whose one term is string.
	{file: "typeparams/typeparams.go", line: 34, message: "Format the number as a decimal"},
}

// Over x/tools' typeparams, which can hold no record, the two make the same findings, each the same in every field,
// but where upstreamWithholdings says the port withholds a fix: there the port's finding is the original's without
// it. The differences tier's harness reads its records from comments, so this one tier keeps its own, built on the
// harness's Findings and Difference. A withholding that matches no fix of the original's there fails, and so does a
// tier that withholds nothing.
func TestPortWithholdsOnlyTheRecordedFixInUpstreamTypeparams(test *testing.T) {
	test.Parallel()
	original, port := parity.Findings(test, stringintconv.Analyzer, Analyzer, analysistest.TestData(), "typeparams")
	test.Logf("the original found %d, the port %d", len(original), len(port))
	// expected is the original's findings, less each fix withheld. Each finding's Fixes is cloned before a fix is
	// taken out of it, so the original's findings are left whole.
	expected := slices.Clone(original)
	withheld := 0
	for _, withholding := range upstreamWithholdings {
		offersIt := func(fix parity.Fix) bool { return fix.Message == withholding.message }
		index := slices.IndexFunc(expected, func(finding parity.Finding) bool {
			return finding.File == withholding.file && finding.Line == withholding.line &&
				slices.ContainsFunc(finding.Fixes, offersIt)
		})
		if index < 0 {
			test.Errorf("%s:%d records %q withheld, and the original offers no such fix there", withholding.file,
				withholding.line, withholding.message)
			continue
		}
		fixIndex := slices.IndexFunc(expected[index].Fixes, offersIt)
		expected[index].Fixes = slices.Delete(slices.Clone(expected[index].Fixes), fixIndex, fixIndex+1)
		withheld++
	}
	for _, missing := range parity.Difference(expected, port) {
		test.Errorf("the original found %s, less what the port withholds, and the port did not", missing)
	}
	for _, extra := range parity.Difference(port, expected) {
		test.Errorf("the port found %s, and the original did not", extra)
	}
	test.Logf("the port withholds %d of the original's fixes", withheld)
	if withheld == 0 {
		test.Fatal("the port withholds nothing in typeparams, so the tier proves nothing")
	}
}
