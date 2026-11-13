package main

// runRecord is one run of the engine, as runs.jsonl keeps it a line each: which run it was, how it ended, what it
// cost, and what its own output said (engineOutput).
type runRecord struct {
	Corpus       string `json:"corpus"`
	CorpusCommit string `json:"corpusCommit"`
	Side         string `json:"side"`
	SideCommit   string `json:"sideCommit"`
	BinarySha256 string `json:"binarySha256"`
	Round        int    `json:"round"`
	Mode         string `json:"mode"`

	// ExitCode is the engine's, -1 when it never started or was ended by a signal. Stopped is whether this command
	// ended it at runDeadline, StartError why it never started, and OutputError why its stdout could not be read.
	ExitCode    int    `json:"exitCode"`
	Stopped     bool   `json:"stopped"`
	StartError  string `json:"startError,omitempty"`
	OutputError string `json:"outputError,omitempty"`

	WallSeconds   float64 `json:"wallSeconds"`
	UserSeconds   float64 `json:"userSeconds"`
	SystemSeconds float64 `json:"systemSeconds"`
	// PeakResidentBytes is the run's peak resident set in bytes, 0 where the host does not say.
	PeakResidentBytes int64 `json:"peakResidentBytes"`
	// LoadBefore is the one-minute load average just before the run started, nil when it could not be read.
	LoadBefore *float64 `json:"loadBefore"`

	engineOutput

	StandardErrorBytes int64 `json:"standardErrorBytes"`

	// ChangedFiles are the tracked files the run changed in its corpus, within the corpus and slash separated, each put
	// back from its commit before the next run (restoreChangedFiles).
	ChangedFiles []string `json:"changedFiles"`
}
