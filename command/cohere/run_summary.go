package main

import "time"

// runFinding is one finding, a rule's or a type error, whose rule is then its TypeScript code (TS2322).
// Both views print the same fields: the human line `path:line:col severity rule message`, and `--json`.
type runFinding struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	// MessageID names which of a rule's messages this is. Empty for a type error.
	MessageID string `json:"messageId,omitempty"`
	Message   string `json:"message"`
}

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

	// Changed is each file the run rewrote, by fixing, formatting or both. WouldChange is the files a
	// `--no-fix` run found a fix or formatting would rewrite, which fail it.
	Changed     []changedFile
	WouldChange int

	// FilesCohered is the files this run checked fresh, and FilesCached the files whose results came from
	// the cache. Cohered is the brand's verb, and the split is the point: a warm run that cohered three
	// files and took the rest from the cache must not read like one that checked them all again.
	FilesCohered int
	FilesCached  int
	// Nodes is the syntax nodes visited in the cohered files only.
	Nodes int

	Gaps runGaps
}

// changedFile is one file the run rewrote.
type changedFile struct {
	// Path is relative to the project root, as a reader scanning the list would want it.
	Path      string
	Fixed     bool
	Formatted bool
	// FixedBy counts the fixes applied, by the rule that made them.
	FixedBy map[string]int
}

// files is every file the run accounted for, checked fresh or from the cache.
func (summary runSummary) files() int {
	return summary.FilesCohered + summary.FilesCached
}

// filesFixed and filesFormatted count the changed files of each kind; a file can be both.
func (summary runSummary) filesFixed() int {
	count := 0
	for _, file := range summary.Changed {
		if file.Fixed {
			count++
		}
	}
	return count
}

func (summary runSummary) filesFormatted() int {
	count := 0
	for _, file := range summary.Changed {
		if file.Formatted {
			count++
		}
	}
	return count
}

// cacheUse is what the run took from the cache.
type cacheUse struct {
	// Replayed is a run answered whole from the run cache, which ran no phase.
	Replayed bool `json:"replayed"`
	// FilesReplayed is the files whose findings came from the cache in a run that computed the rest.
	FilesReplayed int `json:"filesReplayed"`
	// Off is `--no-cache`: nothing read, nothing written.
	Off bool `json:"off"`
}

// runGaps is every way a run fell short of checking everything. Each is said in the footer even when
// the run is green.
type runGaps struct {
	// FilesInScope is set when a run was narrowed to some of the program's files.
	FilesInScope int `json:"filesInScope"`
	CrashedFiles int `json:"crashedFiles"`
	// RulesSkippingEverything is the rules that declined every file they were offered.
	RulesSkippingEverything int  `json:"rulesSkippingEverything"`
	FormattingNotChecked    bool `json:"formattingNotChecked"`
	NothingToCheck          bool `json:"nothingToCheck"`
	// ModifiedBuild is a binary built from a modified tree, which no commit reproduces.
	ModifiedBuild bool `json:"modifiedBuild"`
	// Unread is files the run could not check for any other reason, said as the run said it.
	Unread string `json:"unread,omitempty"`
}

// failed is whether the run found anything, which is what the exit code says too.
func (summary runSummary) failed() bool {
	return summary.TypeErrors > 0 || summary.Findings > 0 || summary.WouldChange > 0
}
