package testing_goroutine

import (
	"slices"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check/analysistest"
	"github.com/system-inc/cohere/internal/go_engine/check/parity"
)

// A go statement that starts a function of the package is judged by that function's body, and reported at the go
// statement, in the caller: here TestStarts starts helper, which calls t.Fatal, so the plain run reports the go
// statement. With helper's body stripped from the type check, as stage 3 would strip it were it standing (#9fbrnra),
// the rule reads no call in helper, and the finding in TestStarts is lost. That dependence is why standing_presence's
// testinggoroutine edge row takes helper out of what stands wherever TestStarts is present (standing_facts'
// TestAStandingBodyAPresentTestStartsLeavesForPresence, over the same shape).
func TestAStrippedHelperLosesTheGoStatementsFinding(test *testing.T) {
	test.Parallel()
	plainFindings, strippedFindings := parity.StrippedFindings(test, Analyzer, analysistest.TestData(),
		parity.Strip{Functions: []string{"helper"}}, "stripped")
	reported := "call to (*testing.T).Fatal from a non-test goroutine (helper calls (*testing.T).Fatal)"
	if messages := parity.Messages(plainFindings); !slices.Equal(messages, []string{reported}) ||
		plainFindings[0].Line != 7 {
		test.Fatalf("the plain run found %v, want TestStarts' go statement reported at line 7: %s", plainFindings,
			reported)
	}
	if len(strippedFindings) != 0 {
		test.Errorf("stripping helper's body from the type check found %v, want nothing, since no call is read there",
			strippedFindings)
	}
}
