package correctness_require_declared_walk_test

// discarded is a reporter that drops what analysistest says, for the run whose wants go unmatched on purpose.
type discarded struct{}

func (discarded) Errorf(format string, arguments ...any) {
	// Ignored: this run's wants go unmatched on purpose, so what analysistest says about them is noise.
}
