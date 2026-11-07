package main

import (
	"fmt"
	"time"
)

// runFinding is one finding, a rule's or a type error, whose rule is then its TypeScript code (TS2322).
// Both views print the same fields: the human line `path:line:col severity rule message`, and `--json`.
type runFinding struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	// MessageId names which of a rule's messages this is. Empty for a type error.
	MessageId string `json:"messageId,omitempty"`
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

	// FormatProject is whether this is the format project of a root no TypeScript project holds, which has no
	// program, so the type check and the lint rules it never runs are no gap in it.
	FormatProject bool

	// Total is the run's wall clock, the number a developer waited for.
	Total time.Duration
	// Graph is how long reading, parsing and binding the program took, zero when no graph was built.
	Graph time.Duration
	// Phases is each phase's outcome and time, as the pipeline recorded them.
	Phases []phaseRecord
	// Rules is the rules that ran.
	Rules int
	// Formatting is the part of the fix phase's time with a format in flight, zero when no formatter ran.
	// The fix phase's own time holds it; the footer shows 🪄 as the rest.
	Formatting time.Duration

	Cache cacheUse
	// PackageCache is what the Go engine's per-package findings cache did, nil for every other run.
	PackageCache *packageCacheUse

	TypeErrors int
	Findings   int
	// WarningFindings are included in Findings and reported, but do not fail the verdict.
	WarningFindings int

	// Changed is each file the run rewrote, by fixing, formatting or both: the files it cohered, the
	// brand's verb for altering. WouldChange is the files a `--no-fix` run found a fix or formatting
	// would rewrite, which fail it.
	Changed     []changedFile
	WouldChange int

	// FilesInScope is every file the run was to account for. FilesChecked is the ones it examined fresh,
	// and FilesCached the ones the cache answered for, so the two add up to FilesInScope; countsAgree
	// holds them to it. The split is the point: a warm run that checked three files and took the rest
	// from the cache must not read like one that checked them all again.
	FilesInScope int
	FilesChecked int
	FilesCached  int
	// Nodes is the syntax nodes walked this run, so only in the checked files.
	Nodes int

	// Adamic is the run's readiness, nil for a run that did not lint or did not ask. See readiness.go.
	Adamic *readinessSummary

	// AdamicRequested is --adamic-readiness, so a run that asked and could not measure says why.
	AdamicRequested bool

	// Skips is each rule that declined every file it was offered. Whether one is a gap depends on its cover
	// having run, which uncoveredSkips decides once the phases are known, into Gaps.
	Skips []ruleSkip

	Gaps runGaps
}

// ruleSkip is a rule that declined every file it was offered: why, and the check that reports the same
// thing in its place, empty when none does.
type ruleSkip struct {
	Rule      string
	Reason    string
	CoveredBy string
}

// uncoveredSkips counts the skips that left something unchecked: every skip with no cover, and every
// covered skip whose cover did not run this run. The one cover today is the types phase, which ran only
// when it ran to its end and lint was not cut off behind it, since a bail is a types phase that stopped
// the run rather than one that stood in for a rule.
func (summary runSummary) uncoveredSkips() int {
	count := 0
	for _, skip := range summary.Skips {
		if skip.CoveredBy == "" || !summary.coverRan(skip.CoveredBy) {
			count++
		}
	}
	return count
}

func (summary runSummary) coverRan(cover string) bool {
	if cover != "types" {
		return false
	}
	typesRan, lintCutOff := false, false
	for _, record := range summary.Phases {
		switch {
		case record.Name == phaseTypes && record.Outcome == outcomeRan:
			typesRan = true
		case record.Name == phaseLint && record.Outcome == outcomeNotReached:
			lintCutOff = true
		}
	}
	return typesRan && !lintCutOff
}

// cohered is the files the run rewrote, which is what the 🪄 and 💅 lines above the footer list.
func (summary runSummary) cohered() int {
	return len(summary.Changed)
}

// countsAgree checks that checked and cached add up to every file in scope, so neither count can drift
// from the other unnoticed. The other invariant, that the cohered count is the number of 🪄 and 💅 lines,
// holds by construction, since both come from Changed, and a test pins it.
func (summary runSummary) countsAgree() error {
	if summary.FilesChecked+summary.FilesCached != summary.FilesInScope {
		return fmt.Errorf("%d checked and %d cached do not add up to the %d files in scope",
			summary.FilesChecked, summary.FilesCached, summary.FilesInScope)
	}
	return nil
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

// runGaps is every way a run fell short of checking everything. Each is said in the footer even when
// the run is green.
type runGaps struct {
	// ProgramFiles is the program's size when the run was narrowed to fewer of its files, zero otherwise.
	ProgramFiles int `json:"programFiles"`
	CrashedFiles int `json:"crashedFiles"`
	// RuleCrashes is each rule that crashed on a file, leaving its verdict there missing. Like a crashed
	// file it fails the run: see runCrash.
	RuleCrashes int `json:"ruleCrashes"`
	// Crashes is each crash the two counts above count, named: the file, the rule for a rule crash, and the
	// cause, a panic in a TypeScript run and the engine's own sentence in an engine's. A count with no reason
	// is a gap nobody can act on, so the list is always as long as the two counts together, and it is the
	// same record `--json` prints as a `crash` line.
	Crashes []runCrash `json:"crashes,omitempty"`
	// RulesSkippingEverything is the rules that declined every file they were offered.
	RulesSkippingEverything int  `json:"rulesSkippingEverything"`
	FormattingNotChecked    bool `json:"formattingNotChecked"`
	NothingToCheck          bool `json:"nothingToCheck"`
	// ModifiedBuild is a binary built from a modified tree, which no commit reproduces.
	ModifiedBuild bool `json:"modifiedBuild"`
	// Unread is files the run could not check for any other reason, said as the run said it.
	Unread string `json:"unread,omitempty"`
	// AdamicIgnored is the Adamic `.a` files the program holds that git's ignore rules leave out of the
	// format walk: checked, and never formatted (#6mhafvb).
	AdamicIgnored int `json:"adamicIgnored,omitempty"`
	// MissingGenerated is each generated file the program asks for that does not exist yet, relative to the
	// project root (#wxsqavy). It fails the run: what the missing file declares was never checked.
	MissingGenerated []string `json:"missingGenerated,omitempty"`
	// FixesHeld is a run whose lint fixes were held because an input the program was told to load is missing
	// (#46ern7e, #4jxsbzr): a generated file, or an import, reference or types entry that resolves to nothing.
	// None was proposed or written, and formatting ran alone.
	FixesHeld bool `json:"fixesHeld,omitempty"`
	// UnclaimedSwift is the directories of Swift files no package holds that discovery found beside the one
	// project it ran here (#ybspptb). Nothing checked them, and the run neither fails nor passes on them. It
	// is this invocation's walk, never recorded, so a replay says what its own walk found.
	UnclaimedSwift []unclaimedSwiftDirectory `json:"unclaimedSwift,omitempty"`
	// UncheckedSwift is the Swift files a Swift run's engine listed as not checked for a reason other than
	// git ignoring them, Vendor/ or being a manifest, which today means no target of the package compiles
	// them, less those the runs of the Swift projects below it account for (#ybspptb). A gap, not a failure.
	// Each is named, never only counted: a gap nobody can find is half a gap.
	UncheckedSwift []uncheckedSwiftFile `json:"uncheckedSwift,omitempty"`
	// WithheldInDependencyCgo is each file of a dependency with cgo files whose type errors a Go run withheld,
	// since no cgo tool ran and the faked package "C" is what they are about, sorted by file as the engine sends
	// them. A gap, not a failure: they are a dependency's, not the project's to fix. Each is named, never only
	// counted, with its package and how many errors it held, so a reader can find what went unsaid.
	WithheldInDependencyCgo []withheldCgoFile `json:"withheldInDependencyCgo,omitempty"`
	// WithheldDependencyCgoDeclarations counts the declarations of dependencies with cgo files that every rule
	// was given without their bodies or initial values, since no cgo tool ran. A gap, not a failure, as the
	// files above are. Counted and not named because the engine counts them and names none: they are spread
	// over whole packages, and the count is what says the rules saw less of those dependencies than all of it.
	WithheldDependencyCgoDeclarations int `json:"withheldDependencyCgoDeclarations,omitempty"`
}

// uncheckedSwiftFile is one Swift file of a run's gap: the file as the engine names it, and the engine's reason
// for leaving it out.
type uncheckedSwiftFile struct {
	File   string `json:"file"`
	Reason string `json:"reason"`
}

// withheldCgoFile is one file of a dependency with cgo files whose type errors a Go run withheld: its package,
// the file as the engine names it, and how many errors it held. The same fields the Go engine's types record
// writes (internal/go_engine/contract.WithheldFile), so the record decodes into it and `--json` writes it as is.
type withheldCgoFile struct {
	Package string `json:"package"`
	File    string `json:"file"`
	Errors  int    `json:"errors"`
}

// failed is whether errors, required rewrites, crashes or a missing generated file fail the run. Warnings
// stay in the finding total without failing it, including when the summary is replayed from the run cache.
func (summary runSummary) failed() bool {
	return summary.TypeErrors > 0 || summary.Findings > summary.WarningFindings || summary.WouldChange > 0 ||
		summary.Gaps.CrashedFiles > 0 || summary.Gaps.RuleCrashes > 0 || len(summary.Gaps.MissingGenerated) > 0
}
