// Package reporting stands in for analysistest: another package whose Run takes a reporter, not a testing.TB.
package reporting

// Reporter is anything with an Errorf method, the shape analysistest.Run takes.
type Reporter interface {
	Errorf(format string, arguments ...any)
}

// Run reports through reporter, which fails whatever test it wraps.
func Run(reporter Reporter, patterns ...string) {
	if len(patterns) == 0 {
		reporter.Errorf("no patterns")
	}
}
