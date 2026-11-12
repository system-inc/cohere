package go_engine

import (
	"bytes"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/build_reads"
	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// otherBuilds is the run under every build beside the host's that the census needs (#72n9mq0), each extra build
// checking the whole workspace as it builds there: a run per build, the host's first, from loaded and pass, the host's
// load and the lint pass it wrote. It is nil when no extra build is needed, so such a run checks nothing more, and
// when the builds could not be read, which it notes, and the run is incomplete.
//
// The host's run is its lint pass as it is. Each extra build loads the workspace under its own environment and runs
// the enabled rules over every package, through its own findings cache (extraBuildRun). The extra builds start heaviest
// first (heaviestFirst) and run side by side, as many at once as the width allows (fixedBuildsWidth,
// measuredBuildsWidth), and their runs are taken in build order whatever their width (extraBuildRuns). A file no build
// the toolchain targets compiles (project.Unbuilt) stays the gap the types phase names.
func (engine *pipeline) otherBuilds(loaded *load.Loaded, pass *lintPass) []buildRun {
	host, extras, buildsError := engine.buildsBeside()
	if buildsError != nil {
		engine.otherBuildsFailed(buildsError)
		return nil
	}
	if len(extras) == 0 {
		return nil
	}
	// Once the last build has run, or one has failed, no build is left to ask for what the host kept for them, so it
	// goes before the builds are merged.
	defer pass.result.Host.Release()
	// Before any build decides anything, the host's reads are whole.
	waitStarted := time.Now()
	hostReads := wholeReads(pass.result)
	waited := time.Since(waitStarted)
	ours := engine.oursSet()
	analyzed := map[string]bool{}
	for _, each := range loaded.Packages {
		analyzed[each.ID] = true
	}
	runs := []buildRun{{
		build:      host,
		compiled:   compiledFiles(loaded.Roots, ours),
		analyzed:   analyzed,
		said:       compiledFiles(loaded.Packages, ours),
		result:     pass.result,
		typeErrors: loaded.Diagnostics,
		inputs:     loaded.Inputs,
		reads:      hostReads,
		loading:    time.Duration(engine.hostLoading.Load()),
		linting:    time.Duration(engine.hostLinting.Load()),
		waited:     waited,
	}}
	if engine.session != nil {
		runs[0].files = engine.session.Files()
	}

	// A width the memory decides is measured as the first build's load ends, against the host's live heap measured
	// here, as the builds start, with nothing else running.
	width, fixed := engine.fixedBuildsWidth(len(extras), pass.result.Host)
	// Under `--timing` each build's heap is measured as its own (extraBuildRun), which holds only with every build
	// running alone: a timed run keeps nothing for its builds, so they restore nothing and run one at a time already.
	if engine.timing != nil {
		width, fixed = 1, true
		engine.timedHostLiveBytes = forcedLiveHeapBytes()
	}
	var measure func() int
	if !fixed {
		hostLive := forcedLiveHeapBytes()
		measure = func() int {
			measured, holding := engine.measuredBuildsWidth(len(extras), hostLive)
			engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes = hostLive, holding
			return measured
		}
	}
	extraRuns, extraError := engine.extraBuildRuns(extras, heaviestFirst(host, extras), ours, pass.result.Host, width,
		measure)
	if extraError != nil {
		engine.otherBuildsFailed(extraError)
		return nil
	}
	return append(runs, extraRuns...)
}

// extraBuildRuns is every extra build's run, in build order, at most width of them at once, each into a slot of its
// own, so the merge reads the runs in build order whatever order they ran in, or the first build that failed in
// order, the order they start in, and its error. order is a permutation of extras' indexes (heaviestFirst). With
// measure, the width is the memory's to decide by what the first build to start holds: it runs alone until its load
// has ended, when measure decides the width with nothing else running, and the rest start then, as many at once as it
// allows (#rmzehfg). A first build whose load failed decides one at a time. The width it ran at is
// engine.buildsAtOnce.
//
// One at a time, a build that fails stops the run before any later build in order starts, so their notes are never
// written. Side by side, a later build may already have ended, so each build notes into a buffer of its own, and once
// every build has ended the buffers are written to stderr in order, up to and including the first build that failed,
// and the rest are dropped with the runs after it: the stream and stderr are a run one at a time's, byte for byte, at
// any width. A build after one known to have failed is never started, as it never would be one at a time.
//
// The builds' lints share one gate (lintGate), a budget of one whole build that each takes its weight of once its first
// load has ended, holding its slot while it waits. At width 1 the gate never waits: the one build running holds no
// more than the budget, and gives its weight back before it returns and hands its slot on.
func (engine *pipeline) extraBuildRuns(
	extras []project.Build,
	order []int,
	ours map[string]bool,
	host *check.HostRun,
	width int,
	measure func() int,
) ([]buildRun, error) {
	runs := make([]buildRun, len(extras))
	failures := make([]error, len(extras))
	notes := make([]bytes.Buffer, len(extras))
	// firstFailed is the earliest position in order of a build known to have failed, len(extras) while none has.
	var firstFailed atomic.Int64
	firstFailed.Store(int64(len(extras)))
	// running counts the builds started and not yet ended against limit, the width, one until measure decides it.
	var admitLock sync.Mutex
	admitting := sync.NewCond(&admitLock)
	running, limit := 0, width
	var deciding sync.Once
	// A width that needs no measuring is decided already, and the first build's end changes nothing of it.
	decide := func(loaded bool) {
		if measure == nil {
			return
		}
		deciding.Do(func() {
			decided := 1
			if loaded {
				decided = measure()
			}
			admitLock.Lock()
			limit = decided
			admitting.Broadcast()
			admitLock.Unlock()
		})
	}
	var afterFirstLoad func(loaded bool)
	if measure != nil {
		limit = 1
		afterFirstLoad = decide
	}
	gate := newLintGate(lintBudget)
	var group sync.WaitGroup
	for position, index := range order {
		extra := extras[index]
		admitLock.Lock()
		for running >= limit {
			admitting.Wait()
		}
		running++
		admitLock.Unlock()
		if int64(position) > firstFailed.Load() {
			admitLock.Lock()
			running--
			admitLock.Unlock()
			break
		}
		var afterLoad func(loaded bool)
		if position == 0 {
			afterLoad = afterFirstLoad
		}
		group.Go(func() {
			// A build hands its slot back only once its failure is known, so at width 1 the next build is never
			// started after one that failed.
			defer func() {
				// The first build decides when it never reached its load's end, so the rest are never held.
				decide(false)
				admitLock.Lock()
				running--
				admitting.Broadcast()
				admitLock.Unlock()
			}()
			runs[index], failures[index] = engine.extraBuildRun(extra, ours, host, gate, &notes[index], afterLoad)
			if failures[index] == nil {
				return
			}
			for earliest := firstFailed.Load(); int64(position) < earliest; earliest = firstFailed.Load() {
				if firstFailed.CompareAndSwap(earliest, int64(position)) {
					break
				}
			}
		})
	}
	group.Wait()
	admitLock.Lock()
	engine.buildsAtOnce = min(limit, max(len(extras), 1))
	admitLock.Unlock()
	for _, index := range order {
		// Ignored: a note that cannot reach stderr changes no record, as note says.
		_, _ = engine.standardError.Write(notes[index].Bytes())
		if failures[index] != nil {
			return nil, failures[index]
		}
	}
	return runs, nil
}

// buildsBeside is the host's build and the extra builds the census needs beside it, as describeProject read them
// (readBuilds), so the builds the lint phase checks are the ones the project record names a file checked by. A census
// that judged no file read none, and needs no extra build.
func (engine *pipeline) buildsBeside() (project.Build, []project.Build, error) {
	if engine.described.buildsError != nil {
		return project.Build{}, nil, engine.described.buildsError
	}
	if len(engine.described.builds) == 0 {
		return project.Build{}, nil, nil
	}
	return engine.described.builds[0], engine.described.builds[1:], nil
}

// recordsReads is whether the load and lint record what each package read of other packages (#b2t9xmf): when the host
// keeps its packages for extra builds (keepsForBuilds), and when the run collects the standard library's entries for
// the release's table, which persists each one's reads beside it (#pcv6eff), so a host restoring the package from the
// table keeps it for its builds as it keeps a package it checked. A run that does neither would pay for reads nothing
// reads.
func (engine *pipeline) recordsReads() bool {
	return engine.keepsForBuilds() || engine.request.standardLibrary.collected != nil
}

// keepsForBuilds is whether the host's run keeps its packages' entries for its extra builds to restore from (#b2t9xmf):
// only when the run checks extra builds and the census needs at least one.
func (engine *pipeline) keepsForBuilds() bool {
	_, extras, buildsError := engine.buildsBeside()
	return engine.request.switches["other-builds"] && buildsError == nil && len(extras) > 0
}

// readBuilds is the host's build, as go env's answer reads it, then the extra builds the census needs beside it
// (project.BuildsFor), with the targets the toolchain lists, and why each file no build compiles is left so, by path.
// targets are the settings' `targets`, each `GOOS/GOARCH` a port the toolchain lists or the run is refused: a
// target named and never checked would read as checked (#ntamp9e). Each file no build compiles is left to the types
// phase, which names it as a gap with that reason.
func readBuilds(
	workspace project.Workspace,
	census project.Census,
	goEnvironment *load.GoEnvironment,
	targets []string,
) ([]project.Build, map[string]string, error) {
	buildContext, goFlags := goEnvironment.Target()
	host := project.HostBuild(buildContext, goFlags)
	ports, portsError := project.ReadPorts(workspace.Root, workspace.Environment())
	if portsError != nil {
		return nil, nil, portsError
	}
	var extraTargets []project.Port
	for _, target := range targets {
		operatingSystem, architecture, _ := strings.Cut(target, "/")
		index := slices.IndexFunc(ports, func(port project.Port) bool {
			return port.OperatingSystem == operatingSystem && port.Architecture == architecture
		})
		if index < 0 {
			return nil, nil, refusedTarget{fmt.Errorf("the go block's targets name %s, which go tool dist list does "+
				"not target, so it could never be checked", target)}
		}
		extraTargets = append(extraTargets, ports[index])
	}
	builds, unbuilt, buildsError := project.BuildsFor(census, host, ports, extraTargets)
	if buildsError != nil {
		return nil, nil, buildsError
	}
	unbuiltReasons := make(map[string]string, len(unbuilt))
	for _, each := range unbuilt {
		unbuiltReasons[each.File] = each.Reason
	}
	return builds, unbuiltReasons, nil
}

// refusedTarget is a target the settings name that no port of the toolchain is, which refuses the run before its
// first record rather than leaving it incomplete, since the settings asked for a check that cannot be made.
type refusedTarget struct{ error }

// checkingBuild is the first extra build of builds, the host's first, that compiles file, read from contents, which
// the project record names it checked by. It reports false when none does: no build was read, or the file is one
// BuildsFor leaves unbuilt, which keeps its constraint reason and stays a gap.
func checkingBuild(builds []project.Build, file string, contents []byte) (project.Build, bool, error) {
	if len(builds) < 2 {
		return project.Build{}, false, nil
	}
	for _, extra := range builds[1:] {
		compiled, compileError := extra.Compiles(file, contents)
		if compileError != nil {
			return project.Build{}, false, compileError
		}
		if compiled {
			return extra, true, nil
		}
	}
	return project.Build{}, false, nil
}

// extraBuildRun is one extra build's run: the workspace loaded under its environment, as the rename's platformLoad
// loads a platform, and the run's enabled rules over every package it loaded, as the host's lint runs, through a
// findings cache of the build's own, whose keys hold the build's target, cgo and flags as go env reads them under its
// environment, so they never meet the host's. Its type errors are its load's for every package. A load with any
// analyzes nothing, as the host's lint runs over no load with one, since findings against wrong semantics would be
// noise, and so has a say on no file.
//
// The build's keys read the digests of the files the host's session hashed (CacheSession.Files) rather than hash them
// again, since nearly every file is the host's too. That holds because nothing writes a file between the host's load
// and this one: gofmt, the long-line wrap and the rename all write before the load the lint reads (pipeline.run), the
// wrap's write is followed by a second load whose Restore keys every file again, and a run whose rename wrote reads no
// findings cache here at all.
//
// host is what the host's run kept for the extra builds, nil when it kept nothing: the build restores from it each
// package its own findings cache does not hold whose own inputs are the host's and whose reads hold here (#b2t9xmf),
// unless the run renamed, whose build reads no cache, or measures under `--timing`, whose build has no session.
//
// gate is the extra builds' lint gate, which the build takes its weight of once its first load has ended, by the share
// of its packages that load checked from source (lintWeight), and gives back once its rules end, every reload's
// included, on every path.
//
// notes is the build's own, where everything it notes goes rather than stderr, since builds side by side end in any
// order (extraBuildRuns).
func (engine *pipeline) extraBuildRun(
	extra project.Build,
	ours map[string]bool,
	host *check.HostRun,
	gate *lintGate,
	notes io.Writer,
	afterLoad func(loaded bool),
) (buildRun, error) {
	environment := append(slices.Clone(engine.described.workspace.Environment()), extra.Environment()...)
	var session *check.CacheSession
	// As the host's lint does, a run that renamed reads no findings cache, whose keys are of the bytes it replaced.
	if !engine.renamed {
		var hostFiles cache.FileDigests
		if engine.session != nil {
			hostFiles = engine.session.Files()
		}
		session = engine.cacheSession(environment, hostFiles, notes)
		if session != nil && !engine.keepsHostRestoreOff {
			session.RestoreFromHost(host)
		}
	}
	// As the host's load does (loadWorkspace), a package with no file in scope is loaded and never analyzed. A root no
	// action reaches, a command main whose test variant is analyzed in its place, is left out of the build's load
	// (#ws2ptvf): the host checked it, and its variant carries its files here. The listing reads through the host's
	// directory reads (#yyvstj6).
	caching := load.Caching{
		Held:              engine.held,
		Timing:            engine.timing,
		Analyzes:          engine.described.hasFileInScope,
		AnalyzedRootsOnly: true,
		SharedReads:       engine.reads,
	}
	if session != nil {
		caching.Restorer = session
		if session.HasStore() {
			caching.Listings = session
		}
	}
	var loading time.Duration
	var linting time.Duration
	started := time.Now()
	loaded, loadError := engine.loadPackages(loadRequest{environment: environment, caching: &caching})
	loading += time.Since(started)
	// The first build's load has ended and its rules have not started: it holds none of the run's slots, and nothing
	// else of the run is running, so the width is measured here, before any other build starts (extraBuildRuns).
	if afterLoad != nil {
		afterLoad(loadError == nil)
	}
	if loadError != nil {
		return buildRun{}, fmt.Errorf("loading the build %s: %w", extra, loadError)
	}
	var heldAtLoadEnd uint64
	if engine.timing != nil {
		heldAtLoadEnd = holdingAbove(forcedLiveHeapBytes(), engine.timedHostLiveBytes)
	}
	run := buildRun{
		build:      extra,
		compiled:   compiledFiles(loaded.Roots, ours),
		analyzed:   map[string]bool{},
		said:       map[string]bool{},
		result:     &check.Result{FindingsByRule: map[string]int{}},
		typeErrors: loaded.Diagnostics,
		inputs:     loaded.Inputs,
		loading:    loading,
		// What it held as its load ended is the build's own under `--timing` with any load: a load with type errors
		// holds its packages too.
		heldAtLoadEnd: heldAtLoadEnd,
	}
	if session != nil {
		run.files = session.Files()
	}
	if len(loaded.Diagnostics) > 0 {
		return run, nil
	}
	for _, each := range loaded.Packages {
		run.analyzed[each.ID] = true
	}
	run.said = compiledFiles(loaded.Packages, ours)
	run.packagesLoaded = loaded.PackagesLoaded
	// A package the load checked from source has type maps, which the loader's check sets first on every package it
	// checks, while one whose types came from export data, the build's own findings cache's or the host's run's, or
	// that the load never needed, has none: check.Run's walks of type reads tell the two apart the same way. Its
	// syntax would not, since a package checked from source none of whose files parsed has none either.
	packages.Visit(loaded.Roots, nil, func(each *packages.Package) {
		if each.TypesInfo != nil {
			run.checkedFromSource++
		}
	})
	weight := lintWeight(run.checkedFromSource, run.packagesLoaded)
	engine.lintStepped(extra, lintStepReady, weight)
	waitStarted := time.Now()
	run.lintWaited = gate.take(weight, func() { engine.lintStepped(extra, lintStepQueued, weight) })
	// A build admitted at once waited for nothing, which the record writes by leaving the wait out.
	if run.lintWaited {
		run.lintWait = time.Since(waitStarted)
	}
	engine.lintStepped(extra, lintStepTaken, weight)
	enabled, _ := engine.enabledRules()
	runRules := func(over *load.Loaded) (*check.Result, error) {
		started := time.Now()
		defer func() { linting += time.Since(started) }()
		return check.Run(over.Packages, enabled, check.Options{
			SingleThreaded: engine.request.switches["single-threaded"],
			Pool:           engine.workPool(),
			Cache:          session,
			Withheld:       over.Withheld,
			Bodiless:       over.Bodiless,
			// Nothing reads the build's syntax after its rules: the merge reads only what the run hands it.
			ReleaseAnalyzed: true,
			ReadFile:        engine.sourceReader(nil, over.Rewritten),
			Timing:          engine.timing,
		})
	}
	// The rules and every reload run within one function whose one deferred return of the weight is the only one, so
	// a build that fails while holding weight gives it back by the same line as one that succeeds, and a test that
	// shows the weight comes back covers both: a leak on any path would leave every build behind it waiting for good.
	// It comes back before the build's end, which a test may hold until other builds have ended, and they may be
	// waiting for this weight.
	var stopSampling func() uint64
	if engine.timing != nil {
		stopSampling = heldWhileLinting(lintPeakSampleInterval)
	}
	result, lintError := func() (*check.Result, error) {
		defer func() {
			engine.lintStepped(extra, lintStepGiving, weight)
			gate.giveBack(weight)
		}()
		result, checkError := runRules(loaded)
		// As the host's lint does (reloadedPass), a question that reached a package the cache restored, with no
		// answer stored, has the build loaded again with that package checked from source, until none does. So does a
		// package restored from the host's run whose facts or answers do not hold here (check.Result.Reloads).
		for checkError == nil && len(result.Reloads) > 0 && session != nil && session.Reload(result.Reloads) {
			reloads := strings.Join(result.Reloads, ", ")
			started := time.Now()
			loaded, loadError = engine.loadPackages(loadRequest{environment: environment, caching: &caching})
			loading += time.Since(started)
			if loadError != nil {
				return nil, fmt.Errorf("loading the build %s again to check %s from source: %w", extra, reloads,
					loadError)
			}
			if len(loaded.Diagnostics) > 0 {
				return nil, fmt.Errorf("loading the build %s again to check %s from source found %d type "+
					"errors the first load did not", extra, reloads, len(loaded.Diagnostics))
			}
			result, checkError = runRules(loaded)
		}
		if checkError != nil {
			return nil, fmt.Errorf("running the rules over the build %s: %w", extra, checkError)
		}
		return result, nil
	}()
	if stopSampling != nil {
		run.heldAtLintPeak = holdingAbove(max(stopSampling(), heldAtLoadEnd+engine.timedHostLiveBytes),
			engine.timedHostLiveBytes)
	}
	if lintError != nil {
		return buildRun{}, lintError
	}
	for _, problem := range result.Cache.Problems {
		noteTo(notes, "the findings cache of the build %s: %s", extra, problem)
	}
	run.result = result
	run.loading, run.linting = loading, linting
	if engine.extraBuildEnds != nil {
		if endError := engine.extraBuildEnds(extra, notes); endError != nil {
			return buildRun{}, fmt.Errorf("the build %s: %w", extra, endError)
		}
	}
	return run, nil
}

// wholeReads is result's reads once every type-read walk the run left running has ended (check.Result.ReadWalks):
// read any earlier, a package whose walk is still running reads as unknown.
func wholeReads(result *check.Result) map[*packages.Package]*build_reads.Reads {
	result.ReadWalks.Wait()
	return result.Reads
}

// lintStepped tells a test's lintSteps hook, when one is set, that extra's lint reached step holding or asking weight.
func (engine *pipeline) lintStepped(extra project.Build, step lintStep, weight int) {
	if engine.lintSteps != nil {
		engine.lintSteps(extra, step, weight)
	}
}

// otherBuildsFailed notes why the builds beside the host's were not checked, which leaves the run incomplete: it
// was asked for them, and checked only the host's.
func (engine *pipeline) otherBuildsFailed(cause error) {
	engine.note("the builds beside the host's were not checked: %v", cause)
	engine.incomplete = true
}

// oursSet is every readable file of ours, as the census and the load account for them.
func (engine *pipeline) oursSet() map[string]bool {
	ours := make(map[string]bool, len(engine.described.readableOurs))
	for _, file := range engine.described.readableOurs {
		ours[file] = true
	}
	return ours
}

// compiledFiles is every file of ours the packages compile, by path: their GoFiles, as written, rather than their
// CompiledGoFiles, whose cgo outputs are named per run.
func compiledFiles(compiled []*packages.Package, ours map[string]bool) map[string]bool {
	files := map[string]bool{}
	for _, each := range compiled {
		for _, file := range each.GoFiles {
			if ours[file] {
				files[file] = true
			}
		}
	}
	return files
}

// mergeBuildRuns is where every build's run meets, the host's first (#72n9mq0): each build's findings in scope after
// its own //cohere:ignore comments (outcomeOf), merged into the one verdict the lint phase writes (mergeBuilds), and
// what each build covered and brought, for the lint record. ran are the analyzer rules that ran, the same in every
// build. It keeps the runs too, so a test reads what each build analyzed.
func (engine *pipeline) mergeBuildRuns(runs []buildRun, ran []string) *buildsVerdict {
	engine.buildRuns = runs
	outcomes := make([]buildOutcome, 0, len(runs))
	for _, run := range runs {
		outcomes = append(outcomes, engine.outcomeOf(run, ran))
	}
	verdict := &buildsVerdict{merged: mergeBuilds(outcomes, mergeIfAllRules(engine.registered))}
	for index, run := range runs {
		record := contract.BuildRecord{
			Target:           run.build.String(),
			Host:             index == 0,
			CompiledFiles:    len(run.compiled),
			AnalyzedPackages: len(run.analyzed),
			FindingsAdded:    verdict.merged.added[index],
			FindingsDropped:  verdict.merged.dropped[index],
			// An extra build's alone: the host's run counts neither, so its record writes neither.
			CheckedFromSource: run.checkedFromSource,
			PackagesLoaded:    run.packagesLoaded,
			// An extra build's alone, and under `--timing` alone: 0 otherwise, which the record leaves out.
			HeldAtLoadEndBytes:  run.heldAtLoadEnd,
			HeldAtLintPeakBytes: run.heldAtLintPeak,
		}
		recordTimes(&record, run)
		if run.result != nil {
			record.EntriesKept = run.result.Host.EntriesKept()
			fromHost := run.result.FromHost
			record.RestoredFromHost = len(fromHost.Restored)
			record.WrittenThrough = run.result.Cache.WrittenThrough
			record.DeclinedAtLoad = fromHost.DeclinedAtLoad
			record.DeclinedAtCheck = fromHost.DeclinedAtCheck
			if len(fromHost.DeclinedBy) > 0 {
				record.DeclinedBy = fromHost.DeclinedBy
			}
			// Every round an extra build was loaded again is its record's, for a decline or a missing answer alike;
			// the host's own rounds are its lint cache record's.
			if index > 0 {
				record.ReloadRounds = run.result.Cache.ReloadRounds
				if len(run.result.Cache.ReloadRoundsBy) > 0 {
					record.ReloadRoundsBy = run.result.Cache.ReloadRoundsBy
				}
			}
		}
		verdict.records = append(verdict.records, record)
	}
	engine.recordBuildCoverage(runs, verdict.records)
	return verdict
}

// recordTimes writes into record where run's time went: its load and its lint, every reload's included, the host's wait
// for its walks before the extra builds could start, an extra build's lint's wait for its weight at the gate, and of
// its result, an extra build's deciding which packages restore from the host's run and the host's keeping its packages
// for the others, each in wall milliseconds, as the record writes them, and whether the build's lint waited at all.
//
// Mutates: record gains run's times
func recordTimes(record *contract.BuildRecord, run buildRun) {
	record.LoadMilliseconds = int(run.loading.Milliseconds())
	record.LintMilliseconds = int(run.linting.Milliseconds())
	record.WaitMilliseconds = int(run.waited.Milliseconds())
	record.LintWaitMilliseconds = int(run.lintWait.Milliseconds())
	record.LintWaited = run.lintWaited
	if run.result != nil {
		record.DecideMilliseconds = int(run.result.FromHost.Deciding.Milliseconds())
		record.KeepMilliseconds = int(run.result.Host.Keeping().Milliseconds())
	}
}

// recordBuildCoverage counts, into each build's record, what it alone covered and what it compiled that no rule ran
// on (#ytnvt8k). A file of ours in scope that only one build compiles is that build's alone, counted when a rule ran
// on it. A file the host's build leaves out that no build analyzed was compiled and never linted, as a build whose
// load found a type error analyzes no package, so it is named under the first build that compiles it, with why,
// and the front door names it as not checked rather than as checked by that build.
//
// Mutates: records gain each build's FilesAlone, NotLinted and NotLintedReason
func (engine *pipeline) recordBuildCoverage(runs []buildRun, records []contract.BuildRecord) {
	inScope := func(file string) bool {
		return engine.described.inScopeSet[file] || engine.described.constrainedInScope[file]
	}
	compiledBy := map[string][]int{}
	for index, run := range runs {
		for file := range run.compiled {
			if inScope(file) {
				compiledBy[file] = append(compiledBy[file], index)
			}
		}
	}
	for file, builds := range compiledBy {
		slices.Sort(builds)
		linted := slices.ContainsFunc(builds, func(index int) bool { return runs[index].said[file] })
		if len(builds) == 1 && linted {
			records[builds[0]].FilesAlone++
		}
		if !linted && engine.described.constrainedInScope[file] {
			records[builds[0]].NotLinted = append(records[builds[0]].NotLinted, file)
		}
	}
	for index := range records {
		if len(records[index].NotLinted) == 0 {
			continue
		}
		slices.Sort(records[index].NotLinted)
		reason := "no build that compiles it analyzed its package"
		if len(runs[index].analyzed) == 0 && len(runs[index].typeErrors) > 0 {
			found := "a type error"
			if len(runs[index].typeErrors) > 1 {
				found = fmt.Sprintf("%d type errors", len(runs[index].typeErrors))
			}
			reason = "the " + runs[index].build.String() + " build compiles it, and its load found " + found +
				", so it analyzed no package"
		}
		records[index].NotLintedReason = reason
	}
}
