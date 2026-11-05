package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestUnusedReportFindsTheUnusedFunctionAndNotTheUsedOne runs the U1000 adapter over a fixture holding one
// function nothing calls, which carries a want, and one an exported function calls, which must stay quiet.
// analysistest fails on a want nothing matched and on a finding no want expected, so both directions hold.
func TestUnusedReportFindsTheUnusedFunctionAndNotTheUsedOne(test *testing.T) {
	test.Parallel()
	results := analysistest.Run(errorsOnly{test}, analysistest.TestData(), unusedReport, "unusedfixture")
	if len(results) != 1 {
		test.Fatalf("the fixture loaded as %d packages, expected 1", len(results))
	}
	if results[0].Err != nil {
		test.Fatalf("running the adapter over the fixture: %v", results[0].Err)
	}
	if len(results[0].Diagnostics) != 1 {
		test.Fatalf("the fixture drew %d findings, expected 1", len(results[0].Diagnostics))
	}
	if results[0].Diagnostics[0].Category != "U1000" {
		test.Errorf("the finding's message id is %q, expected U1000", results[0].Diagnostics[0].Category)
	}
}
