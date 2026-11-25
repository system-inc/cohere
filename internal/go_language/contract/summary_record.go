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

	// UnguardedWorkspaceSums is additive within contract 3 (#34r3v1y), the Go engine's alone: each go.work.sum a go
	// list of the run ran beside with no side copy of its go.work, since none could answer exactly as the tree's, so go
	// may have rewritten it, sorted, and left out when there is none. A gap, never a failure: every file was checked,
	// and `complete` does not move. The front door names each.
	UnguardedWorkspaceSums []string `json:"unguardedWorkspaceSums,omitempty"`
}

func (SummaryRecord) kind() Kind { return KindSummary }
