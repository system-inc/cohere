package scanner_error

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// A package-level scanner is judged by its uses in every body of the package, so its finding hangs on bodies other
// than the one that loops on it: here read loops on input's Scan, and check, another body, calls its Err, so the plain
// run finds nothing. With check's body stripped from the type check, as stage 3 would strip it were it standing
// (#9fbrnra), the rule sees no Err and reports input's scanner. That dependence is why standing_presence's
// scannererr row names input, so check leaves what stands wherever read is present (standing_facts'
// TestAStandingBodyCheckingAPresentScannersErrorLeavesForPresence, over the same shape).
func TestAStrippedErrCheckChangesTheScannersFinding(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"check"}}, "stripped")
	if len(plainFindings) != 0 {
		test.Fatalf("the plain run found %v, want nothing, since check calls input's Err", plainFindings)
	}
	reported := `bufio.Scanner "input" is used in Scan loop at line 14 without final check of input.Err()`
	if len(strippedFindings) != 1 || strippedFindings[0].Line != 10 || strippedFindings[0].Message != reported {
		test.Errorf("stripping check's body from the type check found %v, want input's scanner reported at line 10: "+
			"%s", strippedFindings, reported)
	}
}
