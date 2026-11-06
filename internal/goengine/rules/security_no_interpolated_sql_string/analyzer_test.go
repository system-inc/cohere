package security_no_interpolated_sql_string_test

import (
	"testing"

	"github.com/system-inc/cohere/internal/goengine/check/analysistest"
	"github.com/system-inc/cohere/internal/goengine/rules/security_no_interpolated_sql_string"
	"github.com/system-inc/cohere/policy"
)

// The registry's entry is well formed and its name fits the house scheme, so the rule reaches a finding line as a
// name a reader can look up.
func TestRuleIsAWellFormedHouseRule(test *testing.T) {
	test.Parallel()
	if validateError := security_no_interpolated_sql_string.Rule.Validate(); validateError != nil {
		test.Fatal(validateError)
	}
	_, parseError := policy.Naming.ParseRuleName("cohere-go", "security-no-interpolated-sql-string")
	if parseError != nil {
		test.Fatal(parseError)
	}
}

// The flagged fixtures carry a // want on every value written into a query's quotes or after a comparison, on
// every query method of *DB, *Tx and *Conn, through `+`, fmt.Sprintf and a local. analysistest fails on a finding
// no want expects as much as on a want nothing found, so the allowed and lookalike fixtures prove the rule stays
// quiet on bound parameters, names and clauses built from code, numbers, the accepted escape, a reading it cannot
// be sure of, and a Query, Exec or Sprintf that is not database/sql's or fmt's.
func TestAnalyzer(test *testing.T) {
	test.Parallel()
	analysistest.Run(
		errorsOnly{test},
		analysistest.TestData(),
		security_no_interpolated_sql_string.Analyzer,
		"flagged",
		"allowed",
		"lookalike",
	)
}

// errorsOnly hands analysistest.Run a reporter that isn't a testing.TB. Given a testing.TB, Run first proves the go
// command works by building a throwaway program, and a runner of prebuilt test binaries refuses a test that builds.
// Loading the fixtures needs only go list.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
