package main

import "time"

// runSummary is everything a finished run can say about itself, gathered in one place so every way of
// printing it reads the same facts: the footer a default run ends with, the full account under
// `--verbose`, and whatever comes next. Rendering is kept out of it, and each renderer is thin.
//
// It holds what ran and what it cost, what was found and changed, how much was checked, what the cache
// supplied, and, kept apart because no renderer may drop it, every way the run fell short of checking
// everything.
type runSummary struct {
	// Label is the project's path in a repository with several. Empty for a project checked on its own.
	Label string

	// Total is the run's wall clock, the number a developer waited for.
	Total time.Duration
	// Phases is each phase's outcome and time, as the pipeline recorded them.
	Phases []phaseRecord
	// Formatting is how long formatting took when it ran, zero when it did not.
	Formatting time.Duration

	Cache cacheUse

	TypeErrors int
	Findings   int

	// FilesFixed and FilesFormatted are the files the run rewrote. WouldChange is the files a `--no-fix`
	// run found a fix or formatting would rewrite, which fail it.
	FilesFixed     int
	FilesFormatted int
	WouldChange    int

	Files int
	Nodes int

	Gaps runGaps
}

// cacheUse is what the run took from the cache.
type cacheUse struct {
	// Replayed is a run answered whole from the run cache, which ran no phase.
	Replayed bool
	// FilesReplayed is the files whose findings came from the cache in a run that computed the rest.
	FilesReplayed int
	// Off is `--no-cache`: nothing read, nothing written.
	Off bool
}

// runGaps is every way a run fell short of checking everything. Each is said in the footer even when
// the run is green.
type runGaps struct {
	// FilesInScope is set when a run was narrowed to some of the program's files.
	FilesInScope int
	CrashedFiles int
	// RulesSkippingEverything is the rules that declined every file they were offered.
	RulesSkippingEverything int
	FormattingNotChecked    bool
	NothingToCheck          bool
	// ModifiedBuild is a binary built from a modified tree, which no commit reproduces.
	ModifiedBuild bool
	// Unread is files the run could not check for any other reason, said as the run said it.
	Unread string
}

// failed is whether the run found anything, which is what the exit code says too.
func (summary runSummary) failed() bool {
	return summary.TypeErrors > 0 || summary.Findings > 0 || summary.WouldChange > 0
}
