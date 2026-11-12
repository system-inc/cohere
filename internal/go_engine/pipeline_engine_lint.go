package go_engine

import (
	"cmp"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/duplicate_functions"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/canonical_function_hash"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/rule"
	"github.com/system-inc/cohere/internal/go_engine/suppression"
)

// lintSuppressionRules are suppression's two rules, which run in the lint phase on the directives themselves.
var lintSuppressionRules = []string{suppression.RuleNoUnusedIgnore, suppression.RuleRequireIgnoreReason}

// runLint is the lint phase: the rules the settings leave on, over the analyzed packages, after a load with no
// type error. Findings outside the scope or in an excluded file are dropped, then the //cohere:ignore comments
// in the files in scope withhold what they cover and report what is wrong with them. A generated file is in
// scope like any other: the driver itself withholds a house rule's findings there and counts them, and a vet or
// tools finding there is still a bug in what ships, so it is written. earlier is the pass the rename already ran
// over this load, written as it is, or nil to run one here. It returns the load its pass ran over: loaded, or the last
// a reload loaded again (reloadedPass), which the inputs record reads.
//
// Under `--other-builds`, every other build the census needs checks what can differ from the host's,
// read beside the host's pass before anything is written, and the phase writes the merge of every build's findings
// (#72n9mq0): the findings, the withheld counts and the crashes are every build's, merged; another build's type
// errors are written as the compiler's findings, since the host's lint ran only on a host load without any; and the
// lint record names each build.
func (engine *pipeline) runLint(loaded *load.Loaded, loadError error, earlier *lintPass) *load.Loaded {
	if !engine.request.requests(contract.PhaseLint) {
		engine.phase(contract.PhaseLint, contract.OutcomeSkipped, 0, detailNotRequested)
		return loaded
	}
	if loadError != nil {
		engine.abandonSpeculation()
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0, "the load failed: "+oneLine(loadError))
		return loaded
	}
	if count := len(loaded.Diagnostics); count > 0 {
		// Whatever the rules beside the load found is dropped unread, as main never ran them.
		engine.abandonSpeculation()
		detail := fmt.Sprintf("types bailed: %d type errors, so lint findings against wrong semantics would be noise",
			count)
		if !engine.request.requests(contract.PhaseTypes) {
			detail = fmt.Sprintf("the load found %d type errors, so lint findings against wrong semantics would be "+
				"noise; run with --types to see them", count)
		}
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0, detail)
		return loaded
	}

	started := time.Now()
	pass := earlier
	if pass == nil && engine.speculation != nil {
		ran := engine.speculativePass()
		pass = &ran
	}
	if pass == nil {
		ran := engine.lint(loaded)
		pass = &ran
		// The pass ran here, so its own time is this phase's; one beside the load is counted only from here, so the
		// phases still follow one another as they read.
		started = time.Now()
	}
	// The load the pass ran over: loaded, or the last a reload loaded again (reloadedPass).
	reloaded, lintedLoad := engine.reloadedPass(*pass, loaded)
	pass = &reloaded
	if pass.checkError != nil {
		engine.note("the rules could not run: %v", pass.checkError)
		engine.phase(contract.PhaseLint, contract.OutcomeNotReached, 0,
			"the rules could not run: "+oneLine(pass.checkError))
		return lintedLoad
	}
	result, outcome := pass.result, pass.outcome
	// A run collecting the standard library's entries for the release's table persists each one's reads beside it once
	// every type-read walk has ended, so the table carries them and a host restoring a package from it can keep it for
	// its extra builds (#pcv6eff).
	if engine.request.standardLibrary.collected != nil && engine.session != nil {
		engine.session.PersistCollectedReads(wholeReads(pass.result))
	}
	var verdict *buildsVerdict
	if engine.request.switches["other-builds"] {
		// The host's reads, pass.result.Reads, are what an extra build will restore a package by (#b2t9xmf). Their
		// type-read walks run beside the host's lint and past its end, so otherBuilds waits for every one before any
		// build decides anything (wholeReads); a run with no extra builds never waits, so the host's verdict never
		// pays for them. A build started while the host's walks run would wait on each package's walk the same way.
		if runs := engine.otherBuilds(lintedLoad, pass); len(runs) > 0 {
			ran := make([]string, 0, len(pass.enabled))
			for _, enabledRule := range pass.enabled {
				ran = append(ran, enabledRule.Name)
			}
			verdict = engine.mergeBuildRuns(runs, ran)
			outcome = suppressedFindings{
				kept:              verdict.merged.kept,
				directiveFindings: verdict.merged.directiveFindings,
				withheldByRule:    verdict.merged.withheldByRule,
			}
			for _, typeError := range verdict.merged.typeErrors {
				engine.write(engine.compilerFinding(typeError))
			}
		}
	}

	findings := make([]contract.FindingRecord, 0, len(outcome.kept)+len(outcome.directiveFindings))
	for _, kept := range outcome.kept {
		severity, _ := engine.severityOf(kept.Rule)
		findings = append(findings, ruleFinding(kept, severity))
	}
	for _, directiveFinding := range outcome.directiveFindings {
		severity, on := engine.severityOf(directiveFinding.Rule)
		if !on {
			continue
		}
		findings = append(findings, contract.FindingRecord{
			Source:      contract.SourceRule,
			File:        directiveFinding.File,
			Line:        directiveFinding.Line,
			Column:      directiveFinding.Column,
			Severity:    severity,
			Rule:        directiveFinding.Rule,
			MessageId:   directiveFinding.MessageId,
			Message:     directiveFinding.Message,
			Fixes:       []contract.Edit{},
			Suggestions: []contract.Suggestion{},
		})
	}
	slices.SortStableFunc(findings, func(left contract.FindingRecord, right contract.FindingRecord) int {
		return cmp.Or(
			cmp.Compare(left.File, right.File),
			cmp.Compare(left.Line, right.Line),
			cmp.Compare(left.Column, right.Column),
			cmp.Compare(left.Rule, right.Rule),
		)
	})
	for _, finding := range findings {
		engine.write(finding)
	}

	record := engine.lintCoverage(result, findings, pass.turnedOff, notConfigured(pass.enabled))
	if verdict != nil {
		// Written above as compiler findings, so the record counts every finding record of the phase (#rw6tg3a).
		record.TypeErrors = len(verdict.merged.typeErrors)
	}
	record.WithheldBySuppression = outcome.withheldByRule
	record.WithheldAsGenerated = result.WithheldAsGenerated
	record.WithheldAsCopied = result.WithheldAsCopied
	record.Cache = contract.LintCache{
		On:           result.Cache.On,
		Hit:          result.Cache.Hit,
		FromBinary:   result.Cache.FromBinary,
		Missed:       result.Cache.Missed,
		Written:      result.Cache.Written,
		Unstorable:   result.Cache.Unstorable,
		FactsDropped: result.Cache.FactsDropped,
		FactsSkipped: result.Cache.FactsSkipped,
		Waves:        result.Cache.Waves,
		Reloads:      result.Cache.Reloads,
		ReloadRounds: result.Cache.ReloadRounds,
		Replays:      result.Replays,
		Problems:     len(result.Cache.Problems),
		WriteFailed:  result.Cache.WriteFailed,
	}
	for _, problem := range result.Cache.Problems {
		engine.note("the findings cache: %s", problem)
	}
	if verdict != nil {
		record.Crashes = make([]contract.Crash, 0, len(verdict.merged.crashes))
		for _, crash := range verdict.merged.crashes {
			record.Crashes = append(record.Crashes, contract.Crash{File: crash.File, Error: crash.Error})
		}
		record.Builds = verdict.records
		// The host's problems and failed writes are counted above, from its result, the first run's; each other
		// build's problems are noted under its name as it ends (extraBuildRun), and they and its failed writes are
		// counted here, so the record holds every note the cache made and every write it lost.
		for _, run := range engine.buildRuns[1:] {
			if run.result != nil {
				record.Cache.Problems += len(run.result.Cache.Problems)
				record.Cache.WriteFailed += run.result.Cache.WriteFailed
			}
		}
		record.BuildsAtOnce = engine.buildsAtOnce
		record.BuildsPoolSize = engine.workPool().Size()
		record.BuildsWidthLiveBytes = engine.buildsWidthLiveBytes
		record.BuildsWidthBuildBytes = engine.buildsWidthBuildBytes
		if engine.buildsWidthBuildBytes > 0 {
			record.BuildsWidthPeakFactor = engine.buildPeakFactor()
		}
	}
	if len(record.Crashes) > 0 {
		engine.incomplete = true
	}
	elapsed := pass.elapsed + time.Since(started)
	if engine.duplicatesRequested() {
		engine.duplicates = engine.duplicatesReport(result)
	}
	record.ElapsedMilliseconds = int(elapsed.Milliseconds())
	engine.write(record)
	engine.phase(contract.PhaseLint, contract.OutcomeRan, elapsed, "")
	return lintedLoad
}

// lintPass is the lint phase's rules over one load before any record is written: what they found, the findings in
// scope the //cohere:ignore comments left, and how long that took, or why the rules could not run.
type lintPass struct {
	result     *check.Result
	enabled    []rule.Rule
	turnedOff  []string
	outcome    suppressedFindings
	checkError error
	elapsed    time.Duration
}

// lint runs the rules the settings leave on over loaded's packages with a file in scope, through the findings cache
// when the run has one and has not renamed, keeps the findings in scope, and applies the //cohere:ignore comments to
// them. The run's own loads analyze only those already (loadWorkspace); the rename's cannot, since its plan reads
// every reference, so a load it proved, which the lint phase reads after it wrote, is narrowed here, and with no
// findings cache, which a renamed run reads none of.
func (engine *pipeline) lint(loaded *load.Loaded) lintPass {
	analyzed := slices.DeleteFunc(slices.Clone(loaded.Packages), func(each *packages.Package) bool {
		return !engine.described.hasFileInScope(each)
	})
	// The host's reads, recorded only when the run has extra builds to restore packages into or collects the standard
	// library's table (recordsReads), walked in the load's slots below its work.
	return engine.lintPackages(analyzed, loaded.Rewritten, check.Options{Withheld: loaded.Withheld,
		Bodiless: loaded.Bodiless, Reads: loaded.Reads, WalkSlots: loaded.WalkSlots})
}

// lintPackages is lint over the packages to analyze, with what the load gives the run in options: its Withheld once
// it has returned, or the Ready and Abandon of a pass beside it. rewritten is the load's rewritten cgo outputs, which
// the rules read in place of the build cache's (see sourceReader).
func (engine *pipeline) lintPackages(
	analyzed []*packages.Package,
	rewritten *load.Rewritten,
	options check.Options,
) lintPass {
	started := time.Now()
	enabled, turnedOff := engine.enabledRules()
	session := engine.session
	if engine.renamed {
		session = nil
	}
	options.SingleThreaded = engine.request.switches["single-threaded"]
	options.Cache = session
	// The host's run keeps its packages' entries for the extra builds to restore from, only when there are any.
	options.KeepForBuilds = session != nil && options.Reads != nil && engine.keepsForBuilds()
	options.Pool = engine.workPool()
	// The lint pass is the load's last reader of the packages' syntax: a rename loads the tree again to plan over,
	// and what follows reads the files' contents the project description holds, and the types.
	options.ReleaseAnalyzed = true
	options.ReadFile = engine.sourceReader(nil, rewritten)
	options.Timing = engine.timing
	options.Counted = engine.described.countedAsWalked
	if engine.duplicatesRequested() {
		options.Report = canonical_function_hash.Analyzer
	}
	result, checkError := check.Run(analyzed, enabled, options)
	defer func() { engine.hostLinting.Add(int64(time.Since(started))) }()
	if checkError != nil {
		return lintPass{checkError: checkError, elapsed: time.Since(started)}
	}
	ran := make([]string, 0, len(enabled))
	for _, enabledRule := range enabled {
		ran = append(ran, enabledRule.Name)
	}
	return lintPass{
		result:    result,
		enabled:   enabled,
		turnedOff: turnedOff,
		outcome:   engine.suppress(ran, engine.inScope(result.Findings), engine.described.readable()),
		elapsed:   time.Since(started),
	}
}

// reloadedPass is pass over loaded, or, when a question of a fact computed on demand reached a package the findings
// cache restored and no store answered it, the pass over a load that checks each such package from source, so the
// question is answered as a run without the cache answers it, again for as long as a pass asks of another such package
// (#w7zmvat wave 3). A pass that asked is no result: it ran on placeholders, handed for every question it asked of
// such a package, so one round names them all. Each round only adds packages to load from source, so it ends; one
// whose every reload was loaded from source already stands, its crashes naming the reload that failed. The load
// returned is the one the pass ran over.
func (engine *pipeline) reloadedPass(pass lintPass, loaded *load.Loaded) (lintPass, *load.Loaded) {
	// Only pass's time outlives the first round, so nothing of the round a reload drops is held while it loads again.
	elapsed := pass.elapsed
	current, currentLoad := pass, loaded
	for current.checkError == nil && len(current.result.Reloads) > 0 && engine.session != nil &&
		engine.session.Reload(current.result.Reloads) {
		reloads := strings.Join(current.result.Reloads, ", ")
		var loadError error
		currentLoad, loadError = engine.loadWorkspace(true)
		switch {
		case loadError != nil:
			engine.abandonSpeculation()
			current = lintPass{checkError: fmt.Errorf("loading again to check %s from source: %w", reloads,
				loadError)}
		case len(currentLoad.Diagnostics) > 0:
			engine.abandonSpeculation()
			current = lintPass{checkError: fmt.Errorf("loading again to check %s from source found %d type errors "+
				"the first load did not", reloads, len(currentLoad.Diagnostics))}
		case engine.speculation != nil:
			current = engine.speculativePass()
		default:
			current = engine.lint(currentLoad)
		}
	}
	// Every round ran after the phase's clock started, which counts it: the pass keeps the time it had.
	current.elapsed = elapsed
	return current, currentLoad
}

// inScope are the findings in a file in scope, in order.
func (engine *pipeline) inScope(found []check.Finding) []check.Finding {
	inScope := make([]check.Finding, 0, len(found))
	for _, each := range found {
		if engine.described.inScopeSet[each.File] {
			inScope = append(inScope, each)
		}
	}
	return inScope
}

// enabledRules are the registered rules the settings leave on, which run, and the names of those they turn off.
func (engine *pipeline) enabledRules() (enabled []rule.Rule, turnedOff []string) {
	for _, registeredRule := range engine.registered {
		if _, on := engine.severityOf(registeredRule.Name); on {
			enabled = append(enabled, registeredRule)
			continue
		}
		turnedOff = append(turnedOff, registeredRule.Name)
	}
	return enabled, turnedOff
}

// cacheSession is the findings cache for the run's enabled rules over a load under environment, whose go env answer
// is goEnvironment, the one the load reads (lintCache), made before the load so the load restores what it holds, whose
// keys read files' digests rather than hashing those files again, and nil when lintCache gives none, as under
// `--no-cache` in a binary with no standard-library entries, or when what every key shares cannot be read, which the
// run says on stderr, and always under `--timing`. A run with extra builds gets a
// session without a store when lintCache gives none, since the host's keeps its packages' entries for the extra
// builds and theirs restore from them (#b2t9xmf); under `--timing` it gets none, so nothing is restored from the host.
// files must be of the files as they are on disk when the load runs (see extraBuildRun). Why the cache sat out is
// noted to notes: stderr for the host's build, and an extra build's own buffer for one (extraBuildRuns).
func (engine *pipeline) cacheSession(
	environment []string,
	goEnvironment *load.GoEnvironment,
	files cache.FileDigests,
	notes io.Writer,
) *check.CacheSession {
	// A run under `--timing` is a measurement, and a package the cache restores, its load's or its rules', runs
	// nothing, so a table over it would read as measured where it is not. Such a run reads and writes no cache at all,
	// the binary's standard-library entries included (#3apgjer).
	if engine.timing != nil {
		return nil
	}
	enabled, _ := engine.enabledRules()
	caching, cachingError := lintCache(engine.request, engine.described.workspace, environment, goEnvironment,
		enabled)
	if cachingError != nil {
		noteTo(notes, "the findings cache sat this run out: %v", cachingError)
	}
	if caching == nil && cachingError == nil && engine.keepsForBuilds() {
		// Nothing is stored or read from a store: the session keeps entries in memory for the extra builds alone.
		caching = &check.Caching{ExtraInputs: newStaticcheckConfigurationInputs().inputs}
	}
	if caching == nil {
		return nil
	}
	caching.Files = files
	// The session's own work on every core runs under the run's one pool's slots, as its loads and checks do.
	caching.Pool = engine.workPool()
	// The editor's save loads its buffer in place of the file (held), so its keys are of the buffer: keyed by the
	// disk's bytes, its entries would be restored by the next run over the file on disk as what that file holds.
	if engine.request.stdinPath != "" {
		caching.Held = engine.held
	}
	if engine.duplicatesRequested() {
		return check.NewCacheSession(caching, enabled, canonical_function_hash.Analyzer)
	}
	return check.NewCacheSession(caching, enabled)
}

// suppressedFindings is what the //cohere:ignore comments left of the lint phase's findings, and what they withheld.
type suppressedFindings struct {
	kept              []check.Finding
	withheld          []check.Finding
	directiveFindings []suppression.DirectiveFinding
	withheldByRule    map[string]int
}

// suppress applies the //cohere:ignore comments of files, readable files in scope, read from the contents the project
// description read, to the findings in scope: every readable file for one build's run, and those a build has a say on
// when builds merge (see outcomeOf). ran are the analyzer rules that ran: a directive naming a rule the settings
// turned off is neither used nor unused. The files are indexed as many at once as there are cores, since each index
// is its own file's.
func (engine *pipeline) suppress(ran []string, inScope []check.Finding, files []string) suppressedFindings {
	indexes := make([]*suppression.Index, len(files))
	slots := make(chan struct{}, runtime.GOMAXPROCS(0))
	var group sync.WaitGroup
	for index, file := range files {
		slots <- struct{}{}
		group.Go(func() {
			defer func() { <-slots }()
			indexes[index] = suppression.Build(file, engine.described.contents[file])
		})
	}
	group.Wait()
	located := make([]suppression.Finding, 0, len(inScope))
	for _, found := range inScope {
		located = append(located, suppression.Finding{Rule: found.Rule, File: found.File, Line: found.Line})
	}
	// The duplicates report's name is known, so a suppression keeping a function in its shape is never an unknown
	// rule, and never ran here, so lint neither applies nor dead-checks it: the report does both
	// (pipeline_duplicates.go).
	known := append(knownRuleNames(engine.registered), duplicate_functions.GoRuleName)
	applied := suppression.Apply(known, ran, indexes, located)

	// Kept holds the located findings no directive covered, in the order given. Whether one is withheld depends
	// only on its rule, file and line, so two findings alike in those are kept or withheld together, and walking
	// both lists in step pairs each kept one with the finding it came from.
	kept := make([]check.Finding, 0, len(applied.Kept))
	var withheld []check.Finding
	next := 0
	for index, found := range inScope {
		if next < len(applied.Kept) && applied.Kept[next] == located[index] {
			kept = append(kept, found)
			next++
			continue
		}
		withheld = append(withheld, found)
	}
	return suppressedFindings{
		kept:              kept,
		withheld:          withheld,
		directiveFindings: applied.DirectiveFindings,
		withheldByRule:    applied.WithheldByRule,
	}
}

// ruleFinding is one analyzer finding as a record. A suggestion is offered only when every edit is in the
// finding's own file, since an edit's offsets are into the file the record names. A naming rule's rename is never
// offered: its one edit renames the declaration alone, so an editor applying it would leave every use of the old name
// broken, and the engine applies it at every reference itself (see renameOf). The message already names the spelling.
func ruleFinding(found check.Finding, severity contract.Severity) contract.FindingRecord {
	suggestions := []contract.Suggestion{}
	offered := found.Suggestions
	if _, renames := renameOf(found); renames {
		offered = nil
	}
	for _, suggestion := range offered {
		edits := make([]contract.Edit, 0, len(suggestion.Edits))
		for _, edit := range suggestion.Edits {
			if edit.File != found.File {
				edits = nil
				break
			}
			edits = append(edits, contract.Edit{Start: edit.Start, End: edit.End, Text: edit.Text})
		}
		if edits != nil {
			suggestions = append(suggestions, contract.Suggestion{Message: suggestion.Message, Fixes: edits})
		}
	}
	return contract.FindingRecord{
		Source:      contract.SourceRule,
		File:        found.File,
		Line:        found.Line,
		Column:      found.Column,
		EndLine:     found.EndLine,
		EndColumn:   found.EndColumn,
		Severity:    severity,
		Rule:        found.Rule,
		MessageId:   found.MessageId,
		Message:     found.Message,
		Fixes:       []contract.Edit{},
		Suggestions: suggestions,
	}
}

// notConfigured are the enabled rules missing an option without which they report nothing, sorted by name. The
// options were set from the settings before the first record, so an empty flag here is one nobody configured.
func notConfigured(enabled []rule.Rule) []string {
	names := []string{}
	for _, enabledRule := range enabled {
		for _, option := range enabledRule.ConfiguredBy {
			if flag := enabledRule.Analyzer.Flags.Lookup(option); flag == nil || flag.Value.String() == "" {
				names = append(names, enabledRule.Name)
				break
			}
		}
	}
	slices.Sort(names)
	return names
}

// lintCoverage is the lint record's counts. The rules are the analyzers and suppression's two, never the format
// rule, which belongs to the fix phase. Of them, the ones the settings turned off listened to no file and are
// named silent, off by the config for every file the rules walked; so are the ones not configured that wrote
// nothing, since a rule missing its options cannot report and is not quiet but unconfigured. The ones that ran
// and wrote no finding, whether they had nothing to say or everything they said was outside the scope or
// withheld, are watched and quiet, unless they crashed on a package; the rest found something. A crash is in
// crashes, which makes the run incomplete.
func (engine *pipeline) lintCoverage(
	result *check.Result, written []contract.FindingRecord, turnedOff []string, unconfigured []string,
) contract.LintRecord {
	writtenByRule := map[string]int{}
	for _, finding := range written {
		writtenByRule[finding.Rule]++
	}
	// Never nil, so a run with nothing off writes `[]` rather than `null`.
	silent := append([]string{}, turnedOff...)
	ranNames := make([]string, 0, len(result.FindingsByRule)+len(lintSuppressionRules))
	for name := range result.FindingsByRule {
		if slices.Contains(unconfigured, name) && writtenByRule[name] == 0 {
			silent = append(silent, name)
			continue
		}
		ranNames = append(ranNames, name)
	}
	for _, name := range lintSuppressionRules {
		if _, on := engine.severityOf(name); on {
			ranNames = append(ranNames, name)
			continue
		}
		silent = append(silent, name)
	}
	slices.Sort(silent)

	record := contract.LintRecord{
		Findings:           len(written),
		RulesRun:           len(ranNames) + len(silent),
		FilesWalked:        result.FilesWalked,
		NodesVisited:       result.NodesVisited,
		RulesSilent:        silent,
		Crashes:            []contract.Crash{},
		RulesScopedOff:     map[string]int{},
		RulesNotConfigured: unconfigured,
		ConfigNote:         engine.settings.Note,
		OffReasons:         engine.settings.OffReasons,
	}
	// A rule that crashed on a package did not watch all of it, so its silence there is not quiet.
	for _, name := range ranNames {
		if writtenByRule[name] == 0 && !slices.Contains(result.CrashedRules, name) {
			record.RulesWatchedAndQuiet++
		}
	}
	if result.FilesWalked > 0 {
		for _, name := range silent {
			if !slices.Contains(unconfigured, name) {
				record.RulesScopedOff[name] = result.FilesWalked
			}
		}
	}
	for _, crash := range result.Crashes {
		record.Crashes = append(record.Crashes, contract.Crash{File: crash.File, Error: crash.Error})
	}
	return record
}
