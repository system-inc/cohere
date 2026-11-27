package main

import "strings"

// raceEntry is one package the gate runs under -race, by its directory under the module root, and why. Its run is a
// go test -run pattern selecting the tests of the package the -race run runs, empty for all of them, and an entry
// with kept tests runs exactly those instead (keptRun).
type raceEntry struct {
	directory string
	run       string
	// kept, when it isn't nil, is the whole of the package's -race run, each test with why it is kept: every test
	// the guard calls concurrent by its name, its file or its own body, and two for each place the package's own code
	// starts goroutines (TestEveryNarrowedRaceRunKeepsItsPackagesConcurrency).
	kept   *raceKept
	reason string
}

// keptRun is the go test -run pattern that selects exactly the entry's kept tests, built from the list so the pattern
// and the list can't drift apart, or the entry's own run when it keeps no list.
func (entry raceEntry) keptRun() string {
	if entry.kept == nil {
		return entry.run
	}
	names := entry.kept.tests()
	return "^(" + strings.Join(names, "|") + ")$"
}
