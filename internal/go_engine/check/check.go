// Package check is the Go engine's lint phase: every enabled rule over the loaded packages once, through
// checker.Analyze, with facts flowing across packages. One broken analyzer never takes the run down and is never
// silent: a panic, an error or a failed prerequisite becomes a crash, which makes the run incomplete, and every
// other rule's findings still come back.
//
// It takes and returns plain values; the engine turns them into the lint record and finding records.
package check

import (
	"cmp"
	"errors"
	"fmt"
	"go/token"
	"runtime"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/bounded"
	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/check/checker"
	"github.com/system-inc/cohere/internal/go_engine/fact_reach"
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/build_intermediate_representation"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
	"github.com/system-inc/cohere/internal/go_engine/timeline"
	"github.com/system-inc/cohere/internal/go_engine/timing"
)

// Options says how a run schedules its work.
type Options struct {
	// SingleThreaded runs one action at a time, for `--single-threaded`; the findings are the same either way.
	SingleThreaded bool

	// Cache is the per-package findings cache the run reads and writes, or nil for a run that does neither, as
	// under `--no-cache`. The findings are the same either way. The load is given it first (see
	// load.PackagesCached), so the packages it holds were never parsed; a session no load was given opens here,
	// over packages the load checked whole.
	Cache *CacheSession

	// Withheld is the load's Loaded.Withheld, which the cache keeps with each package it saves.
	Withheld map[*packages.Package]load.PackageWithheld

	// Bodiless is the load's Loaded.Bodiless, or Overlap.Bodiless for a run beside the load, which the cache keeps
	// with each package it saves, so a run restoring the package answers a question of a function it declares without
	// a body from its types alone (#b2t9xmf). Nil, every entry says it recorded none, and such a question reloads.
	Bodiless *load.Bodiless

	// ReleaseAnalyzed lets each analyzed package's syntax and type information go once the rules have finished on it,
	// as a dependency's already go, rather than when the run ends; the Result is the same either way. Run then
	// leaves the analyzed packages without their Syntax and TypesInfo, so only a caller that runs once over a load
	// and reads neither afterwards sets it, as the engine does. A caller that runs again over the same load, as the
	// tests do over loads they share, leaves it unset.
	ReleaseAnalyzed bool

	// ReadFile is what every rule's pass.ReadFile reads a file through, or nil for the disk. A caller that holds the
	// bytes its load parsed serves them, so a rule that reads the source, as the long-line rule reads every file it
	// is given, reads exactly what the syntax was parsed from and never reads the disk again. The checker still
	// refuses a file the pass was not given.
	ReadFile func(filename string) ([]byte, error)

	// Ready and Abandon run the rules beside the load rather than after it, on a run whose cache has no store, which
	// restores only the standard library from the system cache, decided before the load checks anything: Ready is
	// load.Overlap's, so no rule starts on a package before the load has made it final, and Abandon, closed, starts
	// nothing more and has Run return ErrAbandoned once what started has finished, for a load whose end says the rules
	// must not run at all (see checker.Options).
	Ready   func(*packages.Package) <-chan struct{}
	Abandon <-chan struct{}

	// Timing is the run's collector under `--timing`, which each analyzer's run on each package is measured into, and
	// each dispatched listener's calls, read by its dispatcher under rule.Timed, or nil for a run that did not ask,
	// which measures nothing (#3apgjer). The findings are the same either way.
	Timing *timing.Collector

	// Counted, when not nil, is which files FilesWalked and NodesVisited count, of those the rules were given; nil
	// counts every one. The engine leaves out its project's files outside the scope, since a package's file the run
	// excludes is still the package's and read with it, and is not one the run checked. The findings, and the walks
	// the findings cache keeps, are the same either way.
	Counted func(file string) bool

	// Report, when not nil, is an analyzer that is no rule, run beside the rules on every package to analyze: it
	// reports nothing, and its result on each package is in Result.Reports. The findings cache keeps that result with
	// the package, so a package it restores still has one, and an entry written without it is a miss. Under
	// `--duplicates` it is the duplicates report's hasher (#jfvhbj7). Its ResultType must be a pointer to a type gob
	// can encode.
	Report *analysis.Analyzer

	// Reads, when not nil, is the load's Loaded.Reads, or Overlap.Reads for a run beside the load: each package's reads
	// of other packages, which the rules' fact reads join (see checker.Options.Reads), for an extra build to restore
	// the package by (#b2t9xmf). The run marks the packages it cannot vouch for, and hands them all back in
	// Result.Reads. The findings are the same either way.
	Reads map[*packages.Package]*build_reads.Reads

	// WalkSlots, when not nil, are the load's slots (load.Loaded.WalkSlots or load.Overlap.WalkSlots), which each
	// type-read walk takes one of below every package's work, so a walk beside the load never displaces its chain.
	// Nil, the walks take slots of their own, as many as there are cores.
	WalkSlots build_reads.Slots

	// Pool is the run's one set of slots, which every load and check of the run shares, the host's and every extra
	// build's (#ckm0hsp): the checker runs each action under one of them (checker.Options.Pool). Nil, the checker's
	// workers alone bound the actions, one per core.
	Pool *bounded.Pool

	// KeepForBuilds has the run keep, in Result.Host, the entry of each package it analyzed from source whose reads can
	// hold, for its extra builds to restore from (#b2t9xmf). It needs Cache and Reads, and only a run with extra builds
	// sets it, since keeping costs an entry's encoding of each package without a store and its bytes until the builds
	// end.
	KeepForBuilds bool

	// ExportFacts has the run hand back, in Result.Facts, every fact each package's analysis exported, dependencies
	// included, and every answer the fact resolver computed, for the census of facts across builds under
	// `--build-dependence` (#0ghk7vj). The findings are the same either way; only that census sets it, since
	// digesting every fact of a run is work no finding needs.
	ExportFacts bool

	// FactReach has the run hand back, in Result.FactReach, what the host's side of each analyzed package is made of
	// for the facts' reach (#aswfm7a): the answers the run holds, the packages' no-return predicates and the objects
	// whose answers hang on the order the run walked a package in (captureFactReach). Only a host's run with extra
	// builds sets it; the findings are the same either way.
	FactReach bool

	// Inheritance, when not nil, is an extra build's take of the host's run for its syntax-only rules (#y6q1x2j): each
	// such rule skips each file it inherits and reports the host's findings there as its own (rule.InheritedFiles),
	// handed every pass under rule.Inherited (see checker.Options.Inheritance). Only an extra build restoring from the
	// host's run sets it (HostRun.Inheritance). The findings are the same either way.
	Inheritance rule.Inheritance

	// FactHosts, when not nil, is an extra build's host's side of each package for the facts' reach (#5vbs1np), by the
	// package's ID, nil for a package it lints whole: handed every pass on that package under rule.FactHost (see
	// checker.Options.FactHosts), so standing_facts compares the host's facts and answers with the build's before any
	// rule runs there. It is called from several actions at once. Only an extra build sets it.
	FactHosts func(packageId string) fact_reach.Host

	// FactReachSettle, when not nil, is the analyzer an extra build settles each package's standing bodies by for the
	// facts' reach (#aswfm7a), standing_facts', which this package never imports: rooted beside the rules on every
	// package the run analyzes from source, never on one it restores, its result on each is in
	// Result.FactReachSettled. A run none of whose rules requires ctrlflow roots it nowhere, and says why in
	// Result.FactReachUnsettled (settlingIn). It is no rule and no report: it reports nothing, an entry holds nothing
	// of it, and no cache key and no finding count reads it (checker.Options.Beside), so the findings and the entries
	// are the same either way. Its failure on a package is in Result.FactReachCrashes, never a finding and never a
	// failed run.
	FactReachSettle *analysis.Analyzer

	// Timeline is told each boundary of the run's analysis and its save under `--timeline`, as `check.analyze.begin`
	// and `.end` and `check.save.begin` and `.end`, the analysis holding the findings cache's first wave and its save,
	// nil for a run that did not ask, which tells nothing (#3v2zp7g). A field rather than package state, so two runs in
	// one process never stamp each other's timeline. The findings are the same either way.
	Timeline func(label string)

	// StandingBodies, when not nil, is an extra build's standing bodies (#e4rd5dw): asked once for each package with
	// StandingAfter's result there, what it answers is handed every pass on the package under rule.Standing (see
	// checker.Options.StandingBodies), so a confined rule hears no standing body and reports the host's findings there
	// as its own. StandingAfter, when StandingBodies is set too, is the analyzer every follower's action on a package
	// follows, a dispatcher's and the representation's build (followsStanding), and whose result there StandingBodies
	// is asked with, standing_facts' on the census path, which this package never imports. Where it is FactReachSettle
	// and the run roots that nowhere (settlingIn), nothing stands. Both are called from several actions at once. Only
	// an extra build sets them, and no package StandingBodies answers anything for is written to the findings cache
	// (CacheSession.markStood).
	StandingBodies func(packageId string, after any) rule.StandingBodies
	StandingAfter  *analysis.Analyzer

	// holdWalk, nil outside tests, is called with each type-read walk's package before it runs (see
	// build_reads.Walks), so a test can read Result.Reads while a walk is held.
	holdWalk func(owner *packages.Package)

	// KeptDependence and DependenceJoining, nil outside tests, are told of each package the host kept, with what its
	// set is computed from, as the host's pass asks its dependence set (HostRun.computeDependence), and of a reader
	// starting to wait for that pass (HostRun.DependenceComputed), so a test can hold a set and see who waits for it.
	KeptDependence    func(owner *packages.Package, restoring Restoring)
	DependenceJoining func()
}

// ErrAbandoned is Run's error when Options.Abandon closed before every rule had run, so the result would be partial.
var ErrAbandoned = errors.New("the rules were abandoned before they finished")

// Finding is one diagnostic of one rule. Positions are of the file as the engine read it, not as a `//line`
// directive renames it, so they agree with the edits' offsets and with the suppression comments in that file.
type Finding struct {
	Rule string
	File string

	// Line and Column are 1-based, the column counted in UTF-8 bytes, as go/token counts it.
	Line   int
	Column int

	// EndLine and EndColumn are where the diagnostic's range ends, exclusive, or 0 when the analyzer gave no end.
	EndLine   int
	EndColumn int

	// MessageId is the diagnostic's Category when it has one, which is where a house rule puts it, else the
	// ported rule's upstream name, else the analyzer's name.
	MessageId string
	Message   string

	Suggestions []Suggestion
}

// Suggestion is one repair an analyzer offered, which the engine offers and never applies.
type Suggestion struct {
	Message string
	Edits   []Edit
}

// Edit replaces the bytes from Start to End of File with Text. The offsets are 0-based UTF-8 bytes. File is
// kept because an analyzer may edit a file of the package other than the one its finding is in.
type Edit struct {
	File  string
	Start int
	End   int
	Text  string
}

// Crash is a rule that did not finish on one package. File is the package's first Go file, since a rule runs
// per package and the record names a file.
type Crash struct {
	File  string
	Error string
}

// Result is one run.
//
// The lint record's counts come from here. RulesRun is rulesRun, FilesWalked is filesWalked, NodesVisited is
// nodesVisited, and len(Findings) is findings before suppression. rulesWatchedAndQuiet counts the rules with 0 in
// FindingsByRule that are not in CrashedRules: only such a rule ran on every package it was given and reported
// nothing. A crashed rule also has 0, from the packages it never finished, and counting it quiet would read a
// rule that saw nothing as a rule that found nothing. rulesSilent, the rules
// that listened to no file at all, is the engine's to name, since only it knows which rules the config kept from
// every file before they reached this run; a run over no files leaves every rule it was given silent.
type Result struct {
	// Findings are every diagnostic of every rule that finished, but a house rule's in a generated or copied file
	// (see WithheldAsGenerated and WithheldAsCopied), each once by rule, position and message, sorted by file, line,
	// column and rule. A rule that crashed on a package contributes no findings for it, since what it reported
	// before it stopped is not what it would have reported.
	Findings []Finding

	// Crashes are each rule that did not finish on a package, once each, sorted by file.
	Crashes []Crash

	RulesRun int

	// FilesWalked is the distinct Go files of the analyzed packages, the files the rules were given, those
	// Options.Counted keeps.
	FilesWalked int

	// NodesVisited is the nodes of one ast.Inspect over those files.
	NodesVisited int

	// FindingsByRule is each rule's number of findings kept, 0 included, by rule name.
	FindingsByRule map[string]int

	// CrashedRules names, once each and sorted, every rule with a crash on at least one package. Whatever its count
	// in FindingsByRule, such a rule is never quiet.
	CrashedRules []string

	// WithheldAsGenerated is, by rule name, how many findings of a house rule were dropped because their file is
	// generated, only for rules that had any. A house rule is about how we write code, which a generator's output
	// does not answer to, while a vet or tools finding there is still a bug in what we ship, so those stay. The
	// engine writes these counts into the lint record, so a run over generated files never reads as clean when it
	// withheld what it found there.
	WithheldAsGenerated map[string]int

	// WithheldAsCopied is the same count for files that declare themselves copies of upstream source (see package
	// copied). A copy is foreign code we carry until upstream ships the fix, and restyling it to the house would bury
	// its diff against upstream, the one thing about it a reader needs; vet, tools and staticcheck still judge it.
	// A file both generated and copied is counted as generated.
	WithheldAsCopied map[string]int

	// Cache is what the per-package findings cache did, all zero on a run without it.
	Cache CacheCounts

	// Reports is Options.Report's result on each analyzed package it finished on, live or restored, nil without one.
	// ReportCrashes are the packages it did not finish on, as Crashes says a rule's, which it never joins: the report
	// is no rule, and its crash leaves the lint phase's verdict whole.
	Reports       map[*packages.Package]any
	ReportCrashes []Crash

	// ReportsRestored is how many of Reports came from the findings cache rather than from an analysis this run.
	ReportsRestored int

	// FactReachSettled is Options.FactReachSettle's result on each package the run analyzed from source that it
	// finished on, by package ID, nil without it. FactReachCrashes are the packages it did not finish on, as Crashes
	// says a rule's, which it never joins: it is no rule, and its crash leaves the lint phase's verdict whole.
	FactReachSettled map[string]any
	FactReachCrashes []Crash

	// FactReachUnsettled is why the run rooted no Options.FactReachSettle though it was set, empty when it did or was
	// not asked to: "ctrlflow not in the run", for rules none of which requires ctrlflow (settlingIn).
	FactReachUnsettled string

	// Reloads are the IDs of the packages a question of a fact computed on demand reached restored, with no answer
	// stored, sorted (#w7zmvat wave 3). Each question was answered with a placeholder, and its pass ran on, so one run
	// names every package its passes need; the result is not the run's, and every package handed a placeholder is a
	// crash in it, never clean. The caller has the session reload them (CacheSession.Reload) and loads and runs again.
	Reloads []string

	// Replays is how many questions of a fact computed on demand the run answered by replaying the push's walk of a
	// package, every kind's (fact_resolver.Counts' Fallbacks), with the cache or without.
	Replays int

	// Reads is Options.Reads, each package's type reads from the load and fact reads from the rules in one, a package
	// marked never when a finding on it names a file not its own, which can change with a line of another package, or
	// when the findings cache restored it, so its rules read nothing this run. Nil when Options.Reads is. The type
	// reads are walked beside the run, and Run returns without waiting for them, so Reads is whole only once
	// ReadWalks.Wait has returned: every reader of Reads waits first.
	Reads map[*packages.Package]*build_reads.Reads

	// ReadWalks are the type-read walks Run left running, nil when Options.Reads is (see Reads).
	ReadWalks *build_reads.Walks

	// Facts are every fact the run's analyses exported under Options.ExportFacts (exportedFacts), nil without it.
	Facts []ExportedFact

	// RestoredAnswers are, under Options.ExportFacts, the answers the run's store holds of each package it restored,
	// as the resolver reads them, so a census can read a restored package's answers by objectpath (#ss4t5g6). Nil
	// without it, and for a package the store holds none of.
	RestoredAnswers map[*packages.Package]*fact_resolver.Answers

	// FactReach is what the run held of its facts for the facts' reach under Options.FactReach (captureFactReach), nil
	// without it.
	FactReach *FactReachCapture

	// Host is what the run kept for its extra builds under Options.KeepForBuilds, nil without it, and FromHost what an
	// extra build's session restored from the host's run (CacheSession.RestoreFromHost), all zero without one.
	Host     *HostRun
	FromHost HostRestoreCounts

	Elapsed time.Duration
}

// findingKey is what makes two findings the same finding.
type findingKey struct {
	rule    string
	file    string
	line    int
	column  int
	message string
}

// Run runs every rule over the analyzed packages, which are load.Loaded.Packages, loaded with every dependency
// from source. The rules are the enabled ones. It returns an error only when the rules cannot be run at all;
// what a rule gets wrong on a package is a Crash. The packages below analyzed come back without their syntax and
// type information, which the run let go once their facts were learned.
//
// With options.Cache, a package the cache holds is restored rather than analyzed: Analyze is given only the packages
// that missed, a restored package's facts reach the importers that missed as a run's would, and a restored package
// to analyze gives its findings and its files' walk from its entry, into the same dedupe and withholding as live
// ones. Every package that missed, with no load error and on which every action finished without one, is written
// once the graph has finished.
func Run(analyzed []*packages.Package, rules []rule.Rule, options Options) (*Result, error) {
	started := time.Now()
	ruleOf := make(map[*analysis.Analyzer]rule.Rule, len(rules))
	analyzers := make([]*analysis.Analyzer, 0, len(rules))
	for _, enabled := range rules {
		if enabled.Analyzer == nil {
			return nil, fmt.Errorf("running rule %s: it has no analyzer", enabled.Name)
		}
		if earlier, taken := ruleOf[enabled.Analyzer]; taken {
			return nil, fmt.Errorf("running rules %s and %s: they share analyzer %s, so no finding could say "+
				"which rule it is", earlier.Name, enabled.Name, enabled.Analyzer.Name)
		}
		ruleOf[enabled.Analyzer] = enabled
		analyzers = append(analyzers, enabled.Analyzer)
	}
	// roots are what the checker runs on every package to analyze: the rules, and the report beside them.
	roots := analyzers
	if options.Report != nil {
		// A root listed twice has one action per package, which rootActions files under its last index alone, leaving
		// the other's empty.
		if reporting, isRule := ruleOf[options.Report]; isRule {
			return nil, fmt.Errorf("running report %s: it is the analyzer of rule %s, so the run could not tell the "+
				"report's result from the rule's", options.Report.Name, reporting.Name)
		}
		roots = append(slices.Clone(analyzers), options.Report)
	}
	// The analyzer the facts' reach settles by, only beside rules that run ctrlflow (settlingIn), is rooted apart from
	// roots (checker.Options.Beside), which a rule's or the report's own root on a package would be instead.
	settler, unsettled := settlingIn(analyzers, options.FactReachSettle)
	if settle := options.FactReachSettle; settle != nil {
		if settling, isRule := ruleOf[settle]; isRule {
			return nil, fmt.Errorf("settling the facts' reach by %s: it is the analyzer of rule %s, so the run could "+
				"not tell its result from the rule's", settle.Name, settling.Name)
		}
		if settle == options.Report {
			return nil, fmt.Errorf("settling the facts' reach by %s: it is the run's report", settle.Name)
		}
	}
	wrapped := roots
	if settler != nil {
		wrapped = append(slices.Clone(roots), settler)
	}
	if wrapError := wrapRuns(wrapped); wrapError != nil {
		return nil, wrapError
	}
	record := &walkRecord{files: map[*packages.Package][]storedFile{}}
	if options.Ready != nil && options.Cache != nil && options.Cache.HasStore() {
		return nil, errors.New("running the rules beside the load: the findings cache's store restores packages the " +
			"load never checks and analyzes in waves, so a run with it waits for the load")
	}
	checkerOptions := DispatchOptions()
	checkerOptions.Sequential, checkerOptions.ReadFile = options.SingleThreaded, options.ReadFile
	checkerOptions.Ready, checkerOptions.Abandon = options.Ready, options.Abandon
	// Timed hands each dispatcher the run's collector, so it bills each listener's calls to the listener's rule.
	checkerOptions.Timing, checkerOptions.Timed = options.Timing, rule.Timed
	checkerOptions.Reads = options.Reads
	checkerOptions.Inheritance = options.Inheritance
	checkerOptions.FactHosts = options.FactHosts
	// The standing bodies follow StandingAfter, on the census path the analyzer the facts' reach settles by, so a run
	// that roots that nowhere (settlingIn) hands none: following it there would run it, and the answers it learns would
	// reach the entries.
	if options.StandingAfter == nil || options.StandingAfter != options.FactReachSettle || settler != nil {
		checkerOptions.StandingBodies, checkerOptions.StandingAfter = options.StandingBodies, options.StandingAfter
	}
	checkerOptions.Beside = settler
	checkerOptions.ListingsRead = listingsRead
	checkerOptions.Pool = options.Pool
	// The walks run beside the rules, and on past Run's return, so no package's walk sits at the end of the run's
	// chain; whoever reads the reads waits for them (Result.ReadWalks).
	var walks *build_reads.Walks
	if options.Reads != nil {
		walks = build_reads.NewWalks(runtime.GOMAXPROCS(0), options.WalkSlots, options.holdWalk, nil)
		checkerOptions.Walks = walks
	}
	if options.ReleaseAnalyzed {
		checkerOptions.Finished = record.take
	}
	// The run's resolver, which reads and keeps answers in the session's store once the session has restored (below).
	resolver := fact_resolver.New(nil)
	checkerOptions.Resolver = resolver
	// A run whose capture the body differential reads records who asked each answer (FactReachCapture.Askers), on
	// every resolver it asks through, before any pass runs.
	if options.FactReach {
		resolver.RecordAskers()
	}
	session := options.Cache
	// A package anything stands in is never written to the store, whose entry would carry the host's findings there.
	if answer := checkerOptions.StandingBodies; answer != nil && session != nil {
		checkerOptions.StandingBodies = func(packageId string, after any) rule.StandingBodies {
			standing := answer(packageId, after)
			if standing != nil {
				session.markStood(packageId)
			}
			return standing
		}
	}
	if options.Report != nil && session != nil && session.known != nil && !session.known.reports[options.Report] {
		// Its entries would hold no report, and a package they restored would come back without one.
		return nil, fmt.Errorf("running report %s: the findings cache was opened without it", options.Report.Name)
	}
	toAnalyze := analyzed
	var graphs []*checker.Graph
	if session != nil {
		if !session.opened {
			var reachable []*packages.Package
			packages.Visit(analyzed, nil, func(visited *packages.Package) { reachable = append(reachable, visited) })
			session.Restore(load.LoaderSource, LoadedSizes(reachable), reachable, analyzed)
		}
		// The resolver reads answers of the packages the session restored, and keeps those it computes for the store.
		// It is made once the session has restored, which lets go of the last run's. Under a run beside the load the
		// packages' types are not final yet, so only each restored package is known, once an action on it restores
		// it, and no store answers a package the run learns, which has none there.
		resolver = fact_resolver.New(session)
		checkerOptions.Resolver, session.resolver = resolver, resolver
		if options.FactReach {
			resolver.RecordAskers()
		}
		session.bodiless = options.Bodiless
		if options.Ready == nil {
			packages.Visit(analyzed, nil, func(visited *packages.Package) { session.know(visited) })
		}
		checkerOptions.Restored, checkerOptions.Restore = session.restoredInWave, session.restore
		// A package restored from the host's run has the analyzers that learn facts run on what it imports, as one
		// analyzed here would, so its fact reads can be asked of what those exported (decideHostRestores).
		checkerOptions.FactsBelowRestored = func(restoring *packages.Package) bool {
			return session.fromHost(restoring) != nil
		}
		// The first wave is analysis too, of the misses with no miss below them, and the save of what it analyzed, so
		// the check's analysis begins before it.
		timeline.Tell(options.Timeline, "check.analyze.begin")
		firstGraph, waveError := session.firstWave(roots, checkerOptions, options.Withheld)
		if waveError != nil {
			return nil, waveError
		}
		if firstGraph != nil {
			graphs = append(graphs, firstGraph)
		}
		// A package to analyze restored from the host's run is analyzed by restoring, so the actions of the analyzers
		// that learn facts run on what it imports, and its fact reads can be asked of what those exported
		// (decideHostRestores).
		toAnalyze = slices.DeleteFunc(slices.Clone(analyzed), func(restoring *packages.Package) bool {
			return session.restoredInWave(restoring) && session.fromHost(restoring) == nil
		})
		// A session without a store counts only what it restored from the system cache, as a run without the cache
		// counts no wave.
		if len(toAnalyze) > 0 && session.HasStore() {
			session.counts.Waves++
		}
	}
	if session == nil {
		timeline.Tell(options.Timeline, "check.analyze.begin")
	}
	graph, analyzeError := checker.Analyze(roots, toAnalyze, checkerOptions)
	timeline.Tell(options.Timeline, "check.analyze.end")
	if analyzeError != nil {
		return nil, fmt.Errorf("analyzing %d packages: %w", len(toAnalyze), analyzeError)
	}
	if abandoned(options.Abandon) {
		return nil, ErrAbandoned
	}
	graphs = append(graphs, graph)
	recordUnreachedTypes(options.Reads, options.Ready, walks)
	// What the packages restored from the host's run read of other packages' answers is decided now, with no pass
	// running, before anything is saved over them.
	var hostReloads []string
	if session != nil {
		hostReloads = session.decideHostRestores(graphs)
	}
	rootsOf := rootActions(roots, analyzed, graphs)

	result := &Result{
		ReadWalks:           walks,
		RulesRun:            len(rules),
		FindingsByRule:      make(map[string]int, len(rules)),
		WithheldAsGenerated: map[string]int{},
		WithheldAsCopied:    map[string]int{},
	}
	for _, enabled := range rules {
		result.FindingsByRule[enabled.Name] = 0
	}
	files := analyzedFiles(analyzed, session, record)
	generated, copies := withheldFiles(files)
	// known is every file of the load, which a finding's message is read for (see namesForeignFile), made only
	// once a package whose reads are recorded has a finding.
	var loadedFiles *knownFiles
	known := func() *knownFiles {
		if loadedFiles == nil {
			loadedFiles = newKnownFiles()
			packages.Visit(analyzed, nil, loadedFiles.addPackage)
		}
		return loadedFiles
	}
	// What the gather reads of each analyzed package, by its index in analyzed, read once rather than once per
	// analyzer.
	gathering := make([]gatheredPackage, len(analyzed))
	for index, analyzedPackage := range analyzed {
		gathering[index] = gatheredPackage{restored: session.restoredOf(analyzedPackage),
			reads: options.Reads[analyzedPackage]}
	}
	// found is each analyzer's findings on each package, by the analyzer's index in analyzers and the package's in
	// analyzed, nil where it crashed, all of them gathered before any is kept, so the findings kept and the set they
	// are deduplicated in are allocated once at their whole size.
	found := make([][][]Finding, len(analyzers))
	foundCount := 0
	seenCrashes := map[Crash]bool{}
	for analyzerIndex, analyzer := range analyzers {
		analyzerRule := ruleOf[analyzer]
		found[analyzerIndex] = make([][]Finding, len(analyzed))
		for packageIndex, analyzedPackage := range analyzed {
			gathered := &gathering[packageIndex]
			var packageFound []Finding
			if gathered.restored != nil {
				packageFound = gathered.restored.analyses[analyzer].findings
			} else {
				action := rootsOf[analyzerIndex][packageIndex]
				if action == nil {
					return nil, fmt.Errorf("reading the analysis of %s: analyzer %s has no action on it",
						analyzedPackage.ID, analyzer.Name)
				}
				if action.Err != nil {
					crash := Crash{File: firstFile(action.Package), Error: crashSentence(analyzerRule, action)}
					if !seenCrashes[crash] {
						seenCrashes[crash] = true
						result.Crashes = append(result.Crashes, crash)
					}
					if !slices.Contains(result.CrashedRules, analyzerRule.Name) {
						result.CrashedRules = append(result.CrashedRules, analyzerRule.Name)
					}
					continue
				}
				packageFound = actionFindings(analyzerRule, action)
			}
			if gathered.reads != nil && len(packageFound) > 0 {
				gathered.markForeignFindings(analyzedPackage, packageFound, known)
			}
			found[analyzerIndex][packageIndex] = packageFound
			foundCount += len(packageFound)
		}
	}
	result.Findings = make([]Finding, 0, foundCount)
	seenFindings := make(map[findingKey]bool, foundCount)
	// In the order of graph.Roots, each analyzer over each package, so of two findings alike the one kept is the
	// one a run without the cache keeps.
	for analyzerIndex, analyzer := range analyzers {
		analyzerRule := ruleOf[analyzer]
		for _, packageFound := range found[analyzerIndex] {
			for _, finding := range packageFound {
				key := findingKey{finding.Rule, finding.File, finding.Line, finding.Column, finding.Message}
				if seenFindings[key] {
					continue
				}
				seenFindings[key] = true
				if analyzerRule.Origin == rule.OriginHouse && generated[finding.File] {
					result.WithheldAsGenerated[finding.Rule]++
					continue
				}
				if analyzerRule.Origin == rule.OriginHouse && copies[finding.File] {
					result.WithheldAsCopied[finding.Rule]++
					continue
				}
				result.Findings = append(result.Findings, finding)
				result.FindingsByRule[finding.Rule]++
			}
		}
	}
	// A run that kept none hands back no slice at all, as one that never allocated it did.
	if len(result.Findings) == 0 {
		result.Findings = nil
	}
	if settler != nil {
		result.FactReachSettled, result.FactReachCrashes = settledOf(graphs)
	}
	result.FactReachUnsettled = unsettled
	if options.Report != nil {
		// The report is the last of roots.
		result.Reports, result.ReportCrashes, result.ReportsRestored = reportsOf(options.Report, analyzed, session,
			rootsOf[len(roots)-1])
	}
	slices.SortFunc(result.Findings, func(left, right Finding) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Rule, right.Rule),
			// The rest only breaks ties, so the same input always reads the same.
			cmp.Compare(left.Message, right.Message),
		)
	})
	if session != nil {
		missingAnswers := session.reloadsOf(session.resolver)
		session.noteReloadReasons(missingAnswers, hostReloads)
		// A package restored from the host's run whose facts or answers do not hold here has no syntax to analyze, so
		// the build is loaded again with it checked from source, as a question's reload loads its package.
		result.Reloads = slices.Compact(slices.Sorted(slices.Values(append(missingAnswers, hostReloads...))))
	}
	if len(result.Reloads) > 0 {
		// The pipeline runs again over a load that checks the reloads from source and drops this result, but a caller
		// that does not must never read it as clean: each package handed a placeholder ran on a guess.
		for _, analyzedPackage := range analyzed {
			if analyzedPackage.Types != nil && resolver.Unsure(analyzedPackage.Types) {
				result.Crashes = append(result.Crashes, Crash{File: firstFile(analyzedPackage), Error: fmt.Sprintf(
					"the rules on %s were handed placeholders for answers of %s, which the run must load from "+
						"source, so it runs again", analyzedPackage.ID, strings.Join(result.Reloads, ", "))})
			}
		}
	}
	slices.SortFunc(result.Crashes, func(left, right Crash) int {
		return cmp.Or(cmp.Compare(left.File, right.File), cmp.Compare(left.Error, right.Error))
	})
	slices.Sort(result.CrashedRules)
	result.FilesWalked, result.NodesVisited = walk(files, options.Counted)
	result.Replays = int(resolver.Fallbacks())
	if options.Reads != nil {
		for recorded, reads := range options.Reads {
			// A package an earlier wave of this run analyzed recorded its reads then; one the cache restored ran
			// nothing, and its entry keeps no reads yet.
			if session != nil && session.restored(recorded) {
				reads.MarkNever(build_reads.RestoredFromCache)
			}
		}
		result.Reads = options.Reads
	}
	// Beside the load, nothing is read, saved or kept of a package before the load has made it final, and nothing at
	// all once the pass is abandoned (awaitLoaded): a restored package's facts and answers read its types, which the
	// load sets from export data on its own goroutine (#641hyrh).
	if session != nil && options.Ready != nil && !session.awaitLoaded(options.Ready, options.Abandon) {
		return nil, ErrAbandoned
	}
	// A restored package's facts by path and its answers are read before the run keeps its packages for the extra
	// builds, which lets the session's answers go.
	var unnamedFacts []ExportedFact
	var restoredAnswers map[*packages.Package]*fact_resolver.Answers
	if options.ExportFacts && session != nil {
		unnamedFacts, restoredAnswers = session.unnamedFacts(), session.restoredAnswers()
	}
	// Read as the rules' passes left the resolver, before the save computes packages whole for the store: that replays
	// walks no rule asked for and keeps an answer of every function they judge, which no body's rules read.
	if options.FactReach {
		result.FactReach = captureFactReach(resolver, analyzed, graphs)
	}
	if session != nil {
		timeline.Tell(options.Timeline, "check.save.begin")
		session.save(graph, options.Withheld, files)
		timeline.Tell(options.Timeline, "check.save.end")
		result.Cache = session.counts
		result.FromHost = session.hostCounts()
		if options.KeepForBuilds && options.Reads != nil {
			result.Host = session.keepForBuilds(graphs, analyzed, options.Reads, options.Withheld, files,
				gatheredFindingsOf(analyzers, analyzed, gathering, rootsOf, found), walks,
				options.KeptDependence)
			if result.Host != nil {
				result.Host.joining = options.DependenceJoining
			}
		}
	}
	if options.ExportFacts {
		result.Facts = append(exportedFacts(graphs), computedAnswers(resolver, analyzed)...)
		result.Facts = append(result.Facts, unnamedFacts...)
		result.RestoredAnswers = restoredAnswers
	}
	result.Elapsed = time.Since(started)
	return result, nil
}

// recordUnreachedTypes starts, on walks, the walk of the type reads of each package of reads that no action reached,
// so nothing walked them as its last action finished (checker.Options.Reads): a dependency no analyzer that learns
// facts runs on, or a root the run does not analyze. Its type information and syntax are still whole, since only the
// checker lets them go, and only for a package it ran on. A package whose walk is claimed already, or marked never, is
// passed over, and one with no type information, whose types came from export data, is left to the load, which marks it
// never. Beside the load, ready is Options.Ready: a package no action reached may still be in its check, as a root the
// run does not analyze can be, so each is read only once it is final.
//
// Every package then ends its run in one of three ways. One checked from source and reached by an action is walked
// by the checker; one checked from source and reached by none, here; one the load knows can never be restored (types
// alone, cgo) or restored from the findings cache, marked never by the load or by Run.
func recordUnreachedTypes(
	reads map[*packages.Package]*build_reads.Reads,
	ready func(*packages.Package) <-chan struct{},
	walks *build_reads.Walks,
) {
	for unreached, packageReads := range reads {
		if packageReads.Claimed() {
			continue
		}
		var final <-chan struct{}
		if ready != nil {
			final = ready(unreached)
		}
		walks.Go(unreached, final, func() {
			if !packageReads.Claim() || unreached.TypesInfo == nil {
				return
			}
			packageReads.Record(unreached.Types, unreached.TypesInfo, unreached.Syntax)
		})
	}
}

// rootActions is each root action of graphs, by its analyzer's index in roots and then its package's index in analyzed,
// nil where no graph ran one. A later graph's action on a package is the one that counts: the second wave analyzes
// again a package of the first that it could not restore. A root on a package analyzed does not hold, as a first
// wave's on a package below them, is one the gather never reads.
func rootActions(
	roots []*analysis.Analyzer,
	analyzed []*packages.Package,
	graphs []*checker.Graph,
) [][]*checker.Action {
	analyzerIndex := make(map[*analysis.Analyzer]int, len(roots))
	for index, root := range roots {
		analyzerIndex[root] = index
	}
	// A package listed twice gets its action at every index it is listed at, since the gather reads each one.
	packageIndexes := make(map[*packages.Package][]int, len(analyzed))
	for index, analyzedPackage := range analyzed {
		packageIndexes[analyzedPackage] = append(packageIndexes[analyzedPackage], index)
	}
	actions := make([][]*checker.Action, len(roots))
	for index := range actions {
		actions[index] = make([]*checker.Action, len(analyzed))
	}
	for _, graph := range graphs {
		for _, action := range graph.Roots {
			rootIndex, isRoot := analyzerIndex[action.Analyzer]
			if !isRoot {
				continue
			}
			for _, packageIndex := range packageIndexes[action.Package] {
				actions[rootIndex][packageIndex] = action
			}
		}
	}
	return actions
}

// gatheredFindingsOf gives the findings the gather made of a root action's diagnostics, found by the analyzer's index
// in analyzers and the package's in analyzed, for an action the gather converted: a rule's on a package the findings
// cache did not restore, which rootsOf holds. Any other action, a dependency's or one a restored entry stood for, it
// has none of.
func gatheredFindingsOf(
	analyzers []*analysis.Analyzer,
	analyzed []*packages.Package,
	gathering []gatheredPackage,
	rootsOf [][]*checker.Action,
	found [][][]Finding,
) func(action *checker.Action) ([]Finding, bool) {
	analyzerIndexes := make(map[*analysis.Analyzer]int, len(analyzers))
	for index, analyzer := range analyzers {
		analyzerIndexes[analyzer] = index
	}
	packageIndexes := make(map[*packages.Package]int, len(analyzed))
	for index, analyzedPackage := range analyzed {
		// A package listed twice was gathered alike at each index, so its first is read.
		if _, listed := packageIndexes[analyzedPackage]; !listed {
			packageIndexes[analyzedPackage] = index
		}
	}
	return func(action *checker.Action) ([]Finding, bool) {
		analyzerIndex, isRule := analyzerIndexes[action.Analyzer]
		packageIndex, isAnalyzed := packageIndexes[action.Package]
		if !isRule || !isAnalyzed || gathering[packageIndex].restored != nil ||
			rootsOf[analyzerIndex][packageIndex] != action {
			return nil, false
		}
		return found[analyzerIndex][packageIndex], true
	}
}

// abandoned is whether abandon is closed, never for a nil one.
func abandoned(abandon <-chan struct{}) bool {
	select {
	case <-abandon:
		return true
	default:
		return false
	}
}

// DispatchOptions is new checker options holding what the engine's dispatchers read of the checker, as every run of the
// rules sets it, Run's and the parity harness's alike, so a port's tests run its rule as the engine does. Dependents
// hands each dispatcher the rules of this run that require it on a package, so it calls only their listeners, never
// those of a rule the run left out that registered on it all the same, and the engine's own representation records the
// expression map only where a rule that reads it runs. Failed and Tolerant let a dispatcher run past a listener's
// requirement failing on a package, so only that listener's rule fails there, as it did walking by itself, never every
// rule on the dispatcher. ForDependents keeps a dispatcher's requirement for its listeners only where a rule of this
// run requires it too, so what only a left-out rule reads never runs. Resolver is a new fact resolver, one for the run
// the options are made for, in which a fact asked on demand, as function_purity's, is computed (#w7zmvat), with no
// store, which Run gives the resolver of a run with the findings cache. Recorded hands each rule its package's reads,
// to record its lookups of other packages in, once Run sets Reads (#b2t9xmf). Inherited hands each syntax-only rule the
// host's findings it inherits, once Run sets Inheritance (#y6q1x2j). FactHost hands each pass on a package the host's
// side of it for the facts' reach, once Run sets FactHosts (#5vbs1np). Standing hands each pass on a package what
// stands there, once Run sets StandingBodies (#e4rd5dw), and FollowsStanding names the actions that follow
// StandingAfter's there: each dispatcher's, and the representation's build, which then leaves unbuilt what stands for
// every rule its dispatcher starts (followsStanding).
func DispatchOptions() *checker.Options {
	return &checker.Options{
		Dependents:      rule.Dependents,
		Failed:          rule.Failed,
		Tolerant:        dispatch.IsDispatcher,
		ForDependents:   dispatch.ListenerRequirement,
		Resolver:        fact_resolver.New(nil),
		Resolved:        rule.Resolved,
		Recorded:        rule.Recorded,
		Inherited:       rule.Inherited,
		FactHost:        rule.FactHost,
		Standing:        rule.Standing,
		FollowsStanding: followsStanding,
	}
}

// followsStanding is whether analyzer's action on a package follows checker.Options.StandingAfter's there (#e4rd5dw):
// a dispatcher's, whose confined rules hear no standing body, and build_intermediate_representation's, so the
// representation is built less what stands for every rule its dispatcher starts rather than whole. Neither is required
// by standing_facts, the analyzer the census path follows, so no follower waits on itself.
func followsStanding(analyzer *analysis.Analyzer) bool {
	return dispatch.IsDispatcher(analyzer) || analyzer == build_intermediate_representation.Analyzer
}

// actionFindings is what action found, as newFinding makes it.
func actionFindings(actionRule rule.Rule, action *checker.Action) []Finding {
	found := make([]Finding, 0, len(action.Diagnostics))
	for _, diagnostic := range action.Diagnostics {
		found = append(found, newFinding(actionRule, action.Package.Fset, diagnostic))
	}
	return found
}

// newFinding is diagnostic as the rule that reported it.
func newFinding(enabled rule.Rule, fileSet *token.FileSet, diagnostic analysis.Diagnostic) Finding {
	start := fileSet.PositionFor(diagnostic.Pos, false)
	found := Finding{
		Rule:      enabled.Name,
		File:      start.Filename,
		Line:      start.Line,
		Column:    start.Column,
		MessageId: cmp.Or(diagnostic.Category, enabled.UpstreamName, enabled.Analyzer.Name),
		Message:   diagnostic.Message,
	}
	if diagnostic.End.IsValid() {
		end := fileSet.PositionFor(diagnostic.End, false)
		found.EndLine, found.EndColumn = end.Line, end.Column
	}
	for _, fix := range diagnostic.SuggestedFixes {
		suggestion := Suggestion{Message: fix.Message}
		for _, textEdit := range fix.TextEdits {
			editStart := fileSet.PositionFor(textEdit.Pos, false)
			edit := Edit{File: editStart.Filename, Start: editStart.Offset, End: editStart.Offset,
				Text: string(textEdit.NewText)}
			// An insertion may leave End unset.
			if textEdit.End.IsValid() {
				edit.End = fileSet.PositionFor(textEdit.End, false).Offset
			}
			suggestion.Edits = append(suggestion.Edits, edit)
		}
		found.Suggestions = append(found.Suggestions, suggestion)
	}
	return found
}

// crashSentence says why action did not finish. When it failed because something it required failed, checker
// names only those prerequisites, so the sentence adds what each of them actually said, which is the cause.
func crashSentence(enabled rule.Rule, action *checker.Action) string {
	var causes []string
	for _, cause := range originatingFailures(action) {
		causes = append(causes, fmt.Sprintf("%s: %v", cause, cause.Err))
	}
	if len(causes) == 0 {
		return fmt.Sprintf("%s: %v", enabled.Name, action.Err)
	}
	slices.Sort(causes)
	return fmt.Sprintf("%s: %v, because %s", enabled.Name, action.Err, strings.Join(causes, "; "))
}

// originatingFailures is every action below action that failed on its own, with everything it required
// finished, rather than because a prerequisite failed. When an action ran past a failed prerequisite (see
// checker.Action.RanPast), as a dispatcher runs past a listener's failed requirement, and failed all the same, the
// failure is its own: the walk stops there and never names what it ran past as the cause.
func originatingFailures(action *checker.Action) []*checker.Action {
	seen := map[*checker.Action]bool{}
	var found []*checker.Action
	var visit func(below *checker.Action)
	visit = func(below *checker.Action) {
		if seen[below] || below.Err == nil {
			return
		}
		seen[below] = true
		prerequisiteFailed := false
		for _, dependency := range below.Deps {
			if dependency.Err != nil && !below.RanPast(dependency) {
				prerequisiteFailed = true
				visit(dependency)
			}
		}
		if !prerequisiteFailed {
			found = append(found, below)
		}
	}
	for _, dependency := range action.Deps {
		visit(dependency)
	}
	return found
}

// firstFile is the file a crash on analyzedPackage names: its first Go file as written, else the first file the
// rules were given, else the package's ID, so a crash never names nothing.
func firstFile(analyzedPackage *packages.Package) string {
	if len(analyzedPackage.GoFiles) > 0 {
		return analyzedPackage.GoFiles[0]
	}
	if len(analyzedPackage.CompiledGoFiles) > 0 {
		return analyzedPackage.CompiledGoFiles[0]
	}
	return analyzedPackage.ID
}

// analyzedFiles is each analyzed package's files as the rules were given them, with each one's walk and whether it
// is generated or copied: from the entry of a package the cache restored, which has no syntax; from the record
// taken as the checker finished a package, which then let go of its syntax; or from the syntax, of a package the
// checker had no action on.
func analyzedFiles(
	analyzed []*packages.Package,
	session *CacheSession,
	record *walkRecord,
) map[*packages.Package][]storedFile {
	files := make(map[*packages.Package][]storedFile, len(analyzed))
	for _, analyzedPackage := range analyzed {
		if restored, hit := session.filesOf(analyzedPackage); hit {
			files[analyzedPackage] = restored
			continue
		}
		if recorded, taken := record.files[analyzedPackage]; taken {
			files[analyzedPackage] = recorded
			continue
		}
		files[analyzedPackage] = storedFiles(analyzedPackage)
	}
	return files
}

// withheldFiles is every file of the analyzed packages that go/ast.IsGenerated reads as generated, a comment line
// `// Code generated ... DO NOT EDIT.` before the package clause, and every one whose header declares it a copy of
// upstream source (see package copied), judged on the syntax the rules were given rather than on a second read of
// the disk, which could disagree with it.
func withheldFiles(files map[*packages.Package][]storedFile) (generated map[string]bool, copies map[string]bool) {
	generated, copies = map[string]bool{}, map[string]bool{}
	for _, packageFiles := range files {
		for _, file := range packageFiles {
			generated[file.Name] = generated[file.Name] || file.Generated
			copies[file.Name] = copies[file.Name] || file.Copied
		}
	}
	return generated, copies
}

// walk counts the distinct files the rules were given, the syntax trees go/packages parsed from each package's
// CompiledGoFiles, and the nodes of one ast.Inspect over them, of the files counted keeps, or of all when it is nil.
func walk(files map[*packages.Package][]storedFile, counted func(file string) bool) (filesWalked int, nodes int) {
	seen := map[string]bool{}
	for _, packageFiles := range files {
		for _, file := range packageFiles {
			if seen[file.Name] || (counted != nil && !counted(file.Name)) {
				continue
			}
			seen[file.Name] = true
			nodes += file.Nodes
		}
	}
	return len(seen), nodes
}

// reportsOf is a report analyzer's result on every analyzed package, from its restored entry or from the action that
// ran it, the packages it crashed on, and how many it restored. actions are the report's root actions by the package's
// index in analyzed (rootActions), nil where none ran.
func reportsOf(
	report *analysis.Analyzer,
	analyzed []*packages.Package,
	session *CacheSession,
	actions []*checker.Action,
) (map[*packages.Package]any, []Crash, int) {
	reports := map[*packages.Package]any{}
	var crashes []Crash
	restoredCount := 0
	for packageIndex, analyzedPackage := range analyzed {
		if restored, hit := session.reportOf(report, analyzedPackage); hit {
			reports[analyzedPackage] = restored
			restoredCount++
			continue
		}
		ran := actions[packageIndex]
		if ran == nil {
			crashes = append(crashes, Crash{File: firstFile(analyzedPackage),
				Error: fmt.Sprintf("%s has no action on %s", report.Name, analyzedPackage.ID)})
			continue
		}
		if ran.Err != nil {
			crashes = append(crashes, Crash{File: firstFile(ran.Package), Error: ran.Err.Error()})
			continue
		}
		reports[analyzedPackage] = ran.Result
	}
	return reports, crashes, restoredCount
}
