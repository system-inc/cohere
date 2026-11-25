package rules

import "testing"

// errorsOnly hands analysistest.Run a reporter that is not a testing.TB. Given a testing.TB, Run first proves
// the go command works by building a throwaway program, and the pool refuses a test that builds anything no
// key covers. Loading the fixture needs only go list, a read-only query.
type errorsOnly struct{ test *testing.T }

func (reporter errorsOnly) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}
