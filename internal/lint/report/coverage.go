package report

import (
	"fmt"
	"time"
)

// Coverage is what a run actually did, as distinct from what it found.
type Coverage struct {
	FilesInProgram int
	FilesChecked   int
	RulesRun       int
	GraphWarm      bool
	Elapsed        time.Duration
}

// String renders the coverage summary that follows every verdict.
func (coverage Coverage) String() string {
	graph := "graph cold"
	if coverage.GraphWarm {
		graph = "graph warm"
	}
	return fmt.Sprintf("%s · %s · %s · %s",
		formatDuration(coverage.Elapsed),
		pluralize(coverage.FilesChecked, "file", "files"),
		pluralize(coverage.RulesRun, "rule", "rules"),
		graph,
	)
}
