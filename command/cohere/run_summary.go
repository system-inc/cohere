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
	// MemoryRaisedToBytes is each limit a Go run raised its heap to after its load, in order, nil when it raised
	// none and for every other run (#5shepvt). A replay raised nothing, so it carries none.
	MemoryRaisedToBytes []int64
	// Builds is every build a Go run checked when it checked more than one, the host's first, nil for a run of one
	// build and every other run. A replay keeps them, since the verdict it repeats was merged from them.
	Builds []checkedBuild
	// BuildsAtOnce is how many of Builds beside the host's ran side by side, 0 when Builds is nil. A replay keeps it
	// with them, as the record of the run that checked them.
	BuildsAtOnce int
	// BuildsWidthLiveBytes is the host's live heap BuildsAtOnce was decided from, 0 when the decision needed none.
	BuildsWidthLiveBytes uint64

	TypeErrors int
	Findings   int
	// WarningFindings are included in Findings and reported, but do not fail the verdict.
	WarningFindings int

	// Changed is each file the run rewrote, by fixing, formatting or both: the files it cohered, the
	// brand's verb for altering. WouldChange is the files a `--no-fix` run found a fix or formatting
	// would rewrite, which fail it, its nested repositories' included: a reader that counts the finding
	// lines counts those too, and the release cut's gate holds the two equal.
	Changed     []changedFile
	WouldChange int
	// NestedWouldChange is the part of WouldChange in nested repositories, each a file their own run would
	// rewrite. WouldChangeConsidered is the files this run's fix phase examined, the scope its own part of
	// WouldChange was found in, and NestedWouldChangeConsidered the nested files the formatter read, the
	// scope of NestedWouldChange. Each count is said against its own scope, never the program's files: a run
	// at adamic's root read "6,394 files would change (3,165 checked)", nested drift added to its own and
	// both set against a program that holds neither its JSON nor its Markdown (#hb90yfp).
	NestedWouldChange           int
	WouldChangeConsidered       int
	NestedWouldChangeConsidered int

	// FilesInScope is every file the run was to account for. FilesChecked is the ones a phase examined fresh,
	// and FilesCached the ones the cache answered for, so the two are at most FilesInScope, and short of it only
	// by files no phase reached, which a gap names (#1hva1ya); countsAgree holds them to it. The split is the
	// point: a warm run that checked three files and took the rest from the cache must not read like one that
	// checked them all again, and a run that checked none must not read like one that checked them all.
	FilesInScope int
	FilesChecked int
	FilesCached  int
	// FilesTypesOnly is the files an ignore naming `"check": "types"` matched, beside FilesInScope and not in it:
	// type-checked, and never linted or formatted, so a reader sees how much of the program only the type check
	// reads (#cxeq6jw). A file a plain ignore matched is in neither, since it is never checked at all.
	FilesTypesOnly int
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

	// Swift is what only a Swift run says of itself, nil for every other run and for a Swift run with nothing to say.
	Swift *swiftSummary

	// EngineBuilds are the engines this invocation built before it checked, which no record or replay keeps,
	// since a build is this invocation's (engine_build.go).
	EngineBuilds []engineBuild `json:"-"`

	// IgnorePatterns are the root's ignorePatterns as this invocation's discovery counted them, which no record
	// keeps, since discovery runs again on every run (ignore_account.go). Findings already counts each dead one
	// (unused_ignore.go).
	IgnorePatterns []ignoreAccount `json:"-"`
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

// countsAgree checks that checked and cached are at most every file in scope, so neither count can drift
// past it unnoticed. The other invariant, that the cohered count is the number of 🪄 and 💅 lines,
// holds by construction, since both come from Changed, and a test pins it.
func (summary runSummary) countsAgree() error {
	if summary.FilesChecked+summary.FilesCached > summary.FilesInScope {
		return fmt.Errorf("%d checked and %d cached are more than the %d files in scope",
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

// uncheckedEngineFile is one file of an engine run's gap: the file as the engine names it, the engine's reason for
// leaving it out, and the language whose engine left it, which the footer's marker names, "Swift files not checked".
// The format phase's own unchecked files are uncheckedFile (format_only.go), a different fact.
type uncheckedEngineFile struct {
	File     string `json:"file"`
	Reason   string `json:"reason"`
	Language string `json:"language"`
}

// notWrittenFile is one file a fix phase did not write: the file as the engine reached it, and its real path, outside
// the root. The same fields the Go engine's fix record writes (internal/go_engine/contract.NotWrittenFile), so the
// record decodes into it and `--json` writes it as is.
type notWrittenFile struct {
	File     string `json:"file"`
	RealPath string `json:"realPath"`
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
// phasesNotRun are the check's phases this summary has no record of: fix, types and lint, the opt-in phases
// aside. Asked only of a run that stopped on an error, whose summary holds the phases it reached, if any; a
// replay records no phase and stopped before nothing.
func (summary runSummary) phasesNotRun() []string {
	var missing []string
	for _, name := range phaseOrder {
		if optInPhases[name] {
			continue
		}
		recorded := false
		for _, record := range summary.Phases {
			recorded = recorded || record.Name == name
		}
		if !recorded {
			missing = append(missing, string(name))
		}
	}
	return missing
}

func (summary runSummary) failed() bool {
	return summary.TypeErrors > 0 || summary.Findings > summary.WarningFindings || summary.WouldChange > 0 ||
		summary.Gaps.CrashedFiles > 0 || summary.Gaps.RuleCrashes > 0 || len(summary.Gaps.MissingGenerated) > 0 ||
		summary.Gaps.nothingChecked()
}

// swiftSummary is what only a Swift run says of itself.
type swiftSummary struct {
	// SdkIndexSeed is what the check of the standalone scripts did with the toolchain's SDK index seed, nil when no
	// seed applied, since the check's store was not empty.
	SdkIndexSeed *sdkIndexSeedUse `json:"sdkIndexSeed,omitempty"`
}

// sdkIndexSeedUse is the seed's key, the name of its folder under the user's caches, and whether the run's store was
// seeded from it rather than indexing the SDK afresh.
type sdkIndexSeedUse struct {
	Key string `json:"key"`
	Hit bool   `json:"hit"`
}
