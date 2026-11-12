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
	"github.com/system-inc/cohere/internal/go_engine/fact_resolver"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/rules/dispatch"
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
	// restores only the standard library from the binary, decided before the load checks anything: Ready is
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

	// holdWalk, nil outside tests, is called with each type-read walk's package before it runs (see
	// build_reads.Walks), so a test can read Result.Reads while a walk is held.
	holdWalk func(owner *packages.Package)
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
		roots = append(slices.Clone(analyzers), options.Report)
	}
	if wrapError := wrapRuns(roots); wrapError != nil {
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
	session := options.Cache
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
			session.Restore(load.LoaderSource, reachable, analyzed)
		}
		// The resolver reads answers of the packages the session restored, and keeps those it computes for the store.
		// It is made once the session has restored, which lets go of the last run's. Under a run beside the load the
		// packages' types are not final yet, so only each restored package is known, once an action on it restores
		// it, and no store answers a package the run learns, which has none there.
		resolver = fact_resolver.New(session)
		checkerOptions.Resolver, session.resolver = resolver, resolver
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
		// A session without a store counts only what it restored from the binary, as a run without the cache counts
		// no wave.
		if len(toAnalyze) > 0 && session.HasStore() {
			session.counts.Waves++
		}
	}
	graph, analyzeError := checker.Analyze(roots, toAnalyze, checkerOptions)
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
	type rootKey struct {
		analyzer        *analysis.Analyzer
		analyzedPackage *packages.Package
	}
	// A later wave's action on a package is the one that counts: the second wave analyzes again a package of the first
	// that it could not restore.
	actionOf := map[rootKey]*checker.Action{}
	for _, waveGraph := range graphs {
		for _, action := range waveGraph.Roots {
			actionOf[rootKey{action.Analyzer, action.Package}] = action
		}
	}

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
	seenFindings := map[findingKey]bool{}
	seenCrashes := map[Crash]bool{}
	// In the order of graph.Roots, each analyzer over each package, so of two findings alike the one kept is the
	// one a run without the cache keeps.
	for _, analyzer := range analyzers {
		analyzerRule := ruleOf[analyzer]
		for _, analyzedPackage := range analyzed {
			found, restored := session.findingsOf(analyzer, analyzedPackage)
			if !restored {
				action, ran := actionOf[rootKey{analyzer, analyzedPackage}]
				if !ran {
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
				found = actionFindings(analyzerRule, action)
			}
			if reads := options.Reads[analyzedPackage]; reads != nil && len(found) > 0 {
				markForeignFindings(reads, analyzedPackage, found, known)
			}
			for _, finding := range found {
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
	if options.Report != nil {
		ranOn := func(analyzedPackage *packages.Package) (*checker.Action, bool) {
			action, ran := actionOf[rootKey{options.Report, analyzedPackage}]
			return action, ran
		}
		result.Reports, result.ReportCrashes, result.ReportsRestored = reportsOf(options.Report, analyzed, session,
			ranOn)
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
	if session != nil {
		session.save(graph, options.Withheld, files)
		result.Cache = session.counts
		result.FromHost = session.hostCounts()
		if options.KeepForBuilds && options.Reads != nil {
			result.Host = session.keepForBuilds(graphs, analyzed, options.Reads, options.Withheld, files)
		}
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

// markForeignFindings marks reads never when a finding of analyzedPackage names a file that is not the package's own:
// the file it is in, a file its message names or one its fix edits (see namesForeignFile). Such a finding can change
// with a line of another package, which no read records. known gives every file of the load.
func markForeignFindings(
	reads *build_reads.Reads,
	analyzedPackage *packages.Package,
	found []Finding,
	known func() *knownFiles,
) {
	if reads.Never() != "" {
		return
	}
	own := ownFiles(analyzedPackage)
	for _, finding := range found {
		if !own[finding.File] || namesForeignFile(finding, own, known()) {
			reads.MarkNever(fmt.Sprintf("a finding of %s names a file of another package", finding.Rule))
			return
		}
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
// to record its lookups of other packages in, once Run sets Reads (#b2t9xmf).
func DispatchOptions() *checker.Options {
	return &checker.Options{
		Dependents:    rule.Dependents,
		Failed:        rule.Failed,
		Tolerant:      dispatch.IsDispatcher,
		ForDependents: dispatch.ListenerRequirement,
		Resolver:      fact_resolver.New(nil),
		Resolved:      rule.Resolved,
		Recorded:      rule.Recorded,
	}
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
// ran it, the packages it crashed on, and how many it restored. action finds the action that ran on a package that
// was not restored.
func reportsOf(
	report *analysis.Analyzer,
	analyzed []*packages.Package,
	session *CacheSession,
	action func(*packages.Package) (*checker.Action, bool),
) (map[*packages.Package]any, []Crash, int) {
	reports := map[*packages.Package]any{}
	var crashes []Crash
	restoredCount := 0
	for _, analyzedPackage := range analyzed {
		if restored, hit := session.reportOf(report, analyzedPackage); hit {
			reports[analyzedPackage] = restored
			restoredCount++
			continue
		}
		ran, found := action(analyzedPackage)
		if !found {
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
