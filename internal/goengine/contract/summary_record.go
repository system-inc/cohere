package contract

// SummaryRecord closes a checking stream. The front door compares each of its fields against what the
// records before it showed.
type SummaryRecord struct {
	// Findings is the number of finding records written.
	Findings int `json:"findings"`
	// Complete is false when any file or requested phase that could have produced a finding did not run.
	Complete bool `json:"complete"`
	// ExitCode is what the engine will exit with.
	ExitCode int `json:"exitCode"`
}

func (SummaryRecord) kind() Kind { return KindSummary }
