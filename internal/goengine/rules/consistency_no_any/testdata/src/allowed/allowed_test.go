package allowed

import "testing"

// reporter forwards to (*testing.T).Errorf, as analysistest's reporters do, and matches testing.TB's Errorf.
type reporter struct{ test *testing.T }

func (reporter reporter) Errorf(format string, arguments ...any) {
	reporter.test.Helper()
	reporter.test.Errorf(format, arguments...)
}

func TestReporter(test *testing.T) {
	test.Parallel()
	if (reporter{test}).test != test {
		test.Fatal("the reporter holds another test")
	}
}
