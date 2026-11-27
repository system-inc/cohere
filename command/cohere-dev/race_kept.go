package main

import "slices"

// raceKept is the tests a narrowed -race run keeps. A race that only a test left out reaches leaves the race gate:
// that test still runs in the plain test step, so what it checks still holds, and only race detection over its
// scenario is lost (#q1v0ern, the Go Square's ruling). A site is a place the package's own code starts goroutines:
// code of the package that runs at once only because it is handed to another package's workers, a closure the
// checker's scheduler or the type-read walks run, is no site of its own, and is watched under -race only as far as
// the kept tests reach it. Nor does a test count as running code at once because its subtests call Parallel: every
// test of a package runs in parallel with the others, which the guard has never called concurrent either, and counting
// parallel subtests would keep nearly every table test, since most of them are.
type raceKept struct {
	// census is the Loom job that counted each claim's width, so the widths can be read back.
	census string
	// concurrent is every test the guard calls concurrent, by name, file or body, with why it runs at once.
	concurrent []raceKeptTest
	// sites is every place the package's own code starts goroutines, each with the tests that drive it.
	sites []raceSite
}

// raceKeptTest is a test a narrowed -race run keeps, and why.
type raceKeptTest struct {
	test   string
	reason string
}

// raceSite is a place a package's own code starts goroutines, as the guard reads it (concurrencySites):
// `<file>:<function>#<ordinal>`, the ordinal counting the function's sites in source order from 1. Each claim is a
// test that drives it at least two goroutines wide.
type raceSite struct {
	site   string
	claims []raceClaim
}

// raceClaim is a kept test that drives a site, and the most goroutines the census saw live there at once while it ran.
type raceClaim struct {
	test  string
	width int
	// from, when it isn't empty, is the directory of another narrowed entry whose run keeps test and drives this
	// site from that package: a site of one package's code another package's kept test drives wide is held to a
	// -race run all the same. Empty is the entry's own package.
	from string
	// census is the Loom job that counted a claim from another entry, whose census is not this list's. Empty for the
	// entry's own tests, counted by the list's census.
	census string
}

// minimumSiteClaims is how many kept tests each site of a narrowed package needs, so one test renamed or changed
// never leaves a site without a race run.
const minimumSiteClaims = 2

// tests is every test the list keeps, each once, sorted.
func (kept *raceKept) tests() []string {
	names := []string{}
	for _, test := range kept.concurrent {
		names = append(names, test.test)
	}
	for _, site := range kept.sites {
		for _, claim := range site.claims {
			// A claim from another entry is that entry's to run.
			if claim.from == "" {
				names = append(names, claim.test)
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}
