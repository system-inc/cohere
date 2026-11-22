package sql_rows_err

import (
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// Package-level rows are judged by their uses in every body of the package, so their finding hangs on bodies other
// than the one that walks them: here next loops on rows' Next, and check, another body, calls their Err, so the plain
// run finds nothing. With check's body stripped from the type check, as stage 3 would strip it were it standing
// (#9fbrnra), the rule sees no Err and reports the query. That dependence is why standing_presence's sqlrowserr row
// names rows, so check leaves what stands wherever next is present (standing_facts'
// TestAStandingBodyCheckingPresentRowsErrorLeavesForPresence, over the same shape).
func TestAStrippedErrCheckChangesTheRowsFinding(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"check"}}, "stripped")
	if len(plainFindings) != 0 {
		test.Fatalf("the plain run found %v, want nothing, since check calls rows' Err", plainFindings)
	}
	reported := `sql.Rows "rows" is used in Next loop at line 13 without final check of rows.Err()`
	if len(strippedFindings) != 1 || strippedFindings[0].Line != 9 || strippedFindings[0].Message != reported {
		test.Errorf("stripping check's body from the type check found %v, want the query reported at line 9: %s",
			strippedFindings, reported)
	}
}
