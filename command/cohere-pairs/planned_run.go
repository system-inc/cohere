package main

import "fmt"

// The modes each side runs in, which name every run's record and raw output: a cache-on run with every .cache in
// the corpus moved aside first, so it is cold and writes the cache, then a `--no-cache` run, which reads and writes
// none.
const (
	modeCache   = "cache"
	modeNoCache = "no-cache"
)

// plannedRun is one run of the engine the schedule holds.
type plannedRun struct {
	corpus string
	round  int
	side   string
	mode   string
}

// name is the run as its raw output files are named: <corpus>-<side>-<round>-<mode>.
func (planned plannedRun) name() string {
	return fmt.Sprintf("%s-%s-%d-%s", planned.corpus, planned.side, planned.round, planned.mode)
}

// schedule is every run of a pair in the order they run: corpus by corpus, round by round, and within a round both
// sides, base first in odd rounds and branch first in even ones, so a machine that warms or slows across a round
// weighs on each side alike; each side's cache-on run before its `--no-cache` run.
func schedule(corpora []string, rounds int) []plannedRun {
	planned := make([]plannedRun, 0, len(corpora)*rounds*4)
	for _, corpusName := range corpora {
		for round := 1; round <= rounds; round++ {
			sides := []string{sideBase, sideBranch}
			if round%2 == 0 {
				sides = []string{sideBranch, sideBase}
			}
			for _, side := range sides {
				for _, mode := range []string{modeCache, modeNoCache} {
					planned = append(planned, plannedRun{corpus: corpusName, round: round, side: side, mode: mode})
				}
			}
		}
	}
	return planned
}
