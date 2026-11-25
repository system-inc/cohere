package go_language

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_language/build_reads"
	"github.com/system-inc/cohere/internal/go_language/cache"
	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/contract"
	"github.com/system-inc/cohere/internal/go_language/fact_reach"
	"github.com/system-inc/cohere/internal/go_language/load"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/go_language/rule"
	"github.com/system-inc/cohere/internal/go_language/rules/unused_code"
	"github.com/system-inc/cohere/internal/go_language/timeline"
)

// renderingCountsVariable, set to 1, has each extra build write to stderr how many of its verdicts' nodes took the
// host's lines and how many it rendered (check.HostRestoreCounts.NodesReused, #r28e4nh), a census for the shape run.
const renderingCountsVariable = "COHERE_RENDERING_COUNTS"

// errorRefusedAtGate is the error a build refused at the share gate ends its load with, since a build before it in
// start order failed (extraBuildRuns). It is never written: the notes and the failure are written only up to and
// including the first build that failed in order, which is before it.
var errorRefusedAtGate = errors.New("refused at the share gate: a build before it in start order failed")

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
func (pipeline *pipeline) otherBuilds(loaded *load.Loaded, pass *lintPass) []buildRun {
	// The standard library's graph under `--build-dependence` ran beside the host's lint, and is over before anything
	// of the builds is measured or started.
	pipeline.request.timeline.Stamp("handover.begin")
	pipeline.awaitStandardLibrary()
	host, extras, buildsError := pipeline.buildsBeside()
	if buildsError != nil {
		pipeline.otherBuildsFailed(buildsError)
		return nil
	}
	if len(extras) == 0 {
		return nil
	}
	// Every record of the host's graph and the standard library's is written by now, so both let go of what only
	// resolution and encoding read before any build is measured. A run with no build to judge returned above, and
	// pays for no resolution it never reads.
	pipeline.sealDependenceGraphs()
	// Once the last build has run, or one has failed, no build is left to ask for what the host kept for them, so it
	// goes before the builds are merged.
	defer pass.result.Host.Release()
	// Before any build decides anything, the host's reads are whole.
	waitStarted := time.Now()
	hostReads := wholeReads(pass.result)
	waited := time.Since(waitStarted)
	pipeline.request.timeline.Stamp("host.readwalks.end")
	// The walks are over, so a plain run's declarations graph for the must-check census holds every package it will,
	// and its records are written beside the builds, joined before the run's record.
	if pass.declarations != nil {
		pipeline.declarationsStepped(declarationsWalksWhole)
		pipeline.plainBits = newPlainBits(pass.declarations, loaded)
		defer pipeline.completePlainBits(pipeline.plainBits, loaded)()
	}
	// The body reach census reads the host's records of what each declaration uses beside each build's judgment.
	if pipeline.takesBodyReach() {
		pipeline.bodyReachHost = newBodyReachHost(pipeline.dependence, hostReads, pass.result.Findings,
			loaded.Packages)
		// The host's side of each package for the facts' reach, made once here, before any build starts, and only read
		// after. The capture goes once it is read: its no-return predicates hold the host's resolver's packages, which
		// must not stay alive through the builds.
		if pass.result.FactReach != nil {
			pipeline.factReachSides = newFactReachSides(pipeline.bodyReachHost, pass.result.FactReach, loaded.Packages)
			pass.result.FactReach = nil
		}
		// The rules confined for the standing bodies, judged once for every build, and only where any body may stand.
		if pipeline.leavesBodiesStanding() {
			enabled, _ := pipeline.enabledRules()
			pipeline.standingConfined = pipeline.standingConfinedRules(enabled)
		}
	}
	// And the host's dependence sets are in its memo, each computed as its package's walk ended, so no build computes
	// one again beside the pass; the walks are over, so this waits for the last sets alone.
	pass.result.Host.DependenceComputed()
	// The host's facts, which each build's are compared with under `--build-dependence` (#0ghk7vj), indexed before any
	// build starts.
	pipeline.indexHostFacts(pass.result.Facts, pass.result.RestoredAnswers)
	ours := pipeline.oursSet()
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
		loading:    time.Duration(pipeline.hostLoading.Load()),
		linting:    time.Duration(pipeline.hostLinting.Load()),
		waited:     waited,
	}}
	if pipeline.session != nil {
		runs[0].files = pipeline.session.Files()
	}

	// A width the memory decides is decided against the host's live heap measured here, as the builds start, with
	// nothing else running. In a run that takes no share gate, one with a findings store whose host missed more than
	// lowShareAtGate of its packages, a cold fill, or one judging each build's dependence (takesShareGate), it is
	// main's rule whole: the first build runs alone and is measured as its load ends. Otherwise it is decided at the
	// share gate (shareGate): from that heap alone when every build's share is low, and otherwise measured as the first
	// build's load ends, each holding budgeted at buildHoldingBudget. Cold with a store, on Shackler, the gate cost
	// about 3 CPU-s at cohere's root at every width tried (#ghb3rcn), cause unmeasured, and warm it bought 0.10 s
	// (#ybjbqm2). The host's counts are final here, its lint saved, so the decision reads them once.
	gated := pipeline.takesShareGate(pass.result.Cache)
	width, fixed := pipeline.fixedBuildsWidth(
		len(extras),
		pass.result.Host,
		pipeline.hostMostlyHitStore(pass.result.Cache),
	)
	// Under `--timing` each build's heap is measured as its own (extraBuildRun), which holds only with every build
	// running alone: a timed run keeps nothing for its builds, so they restore nothing and run one at a time already.
	if pipeline.timing != nil {
		width, fixed = 1, true
		pipeline.timedHostLiveBytes = forcedLiveHeapBytes()
	}
	var fromHostLive func() int
	var measure func() int
	if !fixed {
		hostLive := forcedLiveHeapBytes()
		// Left nil in a run that takes no share gate: one with a findings store whose host mostly missed, or one
		// judging each build's dependence, whose builds restore nothing from the host's run (takesShareGate).
		if gated {
			fromHostLive = func() int {
				decided, slack, holding := pipeline.hostLiveBuildsWidth(len(extras), hostLive)
				pipeline.buildsWidthLiveBytes, pipeline.buildsWidthBuildBytes = hostLive, holding
				pipeline.buildsWidthSlack = slack
				pipeline.buildsWidthFromHostLive = true
				// A test's width in place of the decision, which the record's fields above still explain.
				if pipeline.buildsWidthAtGateForced > 0 {
					decided = min(pipeline.buildsWidthAtGateForced, max(len(extras), 1))
				}
				return decided
			}
		}
		measure = func() int {
			measured, slack, holding := pipeline.measuredBuildsWidth(len(extras), hostLive, gated)
			pipeline.buildsWidthLiveBytes, pipeline.buildsWidthBuildBytes = hostLive, holding
			pipeline.buildsWidthSlack = slack
			// A test's width in place of the gate's fallback too, so a test holds builds at the gate at a width it
			// knows whatever their shares.
			if pipeline.buildsWidthAtGateForced > 0 && gated {
				measured = min(pipeline.buildsWidthAtGateForced, max(len(extras), 1))
			}
			return measured
		}
	}
	pipeline.request.timeline.Stamp("handover.end")
	extraRuns, extraError := pipeline.extraBuildRuns(extras, heaviestFirst(host, extras), ours, pass.result.Host, width,
		fromHostLive, measure)
	pipeline.request.timeline.Stamp("builds.end")
	if extraError != nil {
		pipeline.otherBuildsFailed(extraError)
		return nil
	}
	runs = append(runs, extraRuns...)
	pipeline.writeBodyReachCensus(runs)
	pipeline.writeFromSourceRecord(runs)
	pipeline.writeMustCheckCensus(runs)
	return runs
}

// extraBuildRuns is every extra build's run, in build order, at most width of them at once, each into a slot of its
// own, so the merge reads the runs in build order whatever order they ran in, or the first build that failed in
// order, the order they start in, and its error. order is a permutation of extras' indexes (heaviestFirst). The width
// it ran at is pipeline.buildsAtOnce.
//
// With measure alone, the width is the memory's to decide by what the first build to start holds, main's rule whole, a
// run's that takes no share gate (takesShareGate): it runs alone until its load has ended, when measure decides the
// width, and the rest start then, as many at once as it allows (#rmzehfg). A first build whose load failed decides one
// at a time.
//
// With fromHostLive too, a run's that takes the gate, the width is decided at the share gate (shareGate,
// #ghb3rcn): every build starts at once, lists, builds its graph and restores, and waits before its type check
// (load.Caching.AtGate) until every build has reported its share or ended. When every share is low the width is
// fromHostLive's, decided then. Otherwise it is measure's: the first build in order passes alone, and measure decides
// the width by what it holds as its load ends, which a first build whose load failed decides as one at a time. Builds
// pass in order, each once every build before it has passed or ended and fewer than the width are past the gate. A run
// with a store takes it only when its host mostly restored (takesShareGate, #ybjbqm2): on Shackler it cost about 3
// CPU-s at cohere's root cold with a store at every width tried, cause unmeasured, and read 0.26 s faster without one,
// its CPU level.
//
// With neither the width is fixed, and each build starts once fewer than width are running, in order.
//
// One at a time, a build that fails stops the run before any later build in order starts, so their notes are never
// written. Side by side, a later build may already have ended, so each build notes into a buffer of its own, and once
// every build has ended the buffers are written to stderr in order, up to and including the first build that failed,
// and the rest are dropped with the runs after it: the stream and stderr are a run one at a time's, byte for byte, at
// any width. A build after one known to have failed is never started without the share gate, and is refused at the
// gate (errorRefusedAtGate), so it never type-checks, as it never would one at a time; one before it still runs.
//
// The builds' lints share one gate (lintGate), a budget of one whole build that each takes its weight of once its first
// load has ended, holding its slot while it waits. At width 1 the gate never waits: the one build running holds no
// more than the budget, and gives its weight back before it returns and hands its slot on.
func (pipeline *pipeline) extraBuildRuns(
	extras []project.Build,
	order []int,
	ours map[string]bool,
	host *check.HostRun,
	width int,
	fromHostLive func() int,
	measure func() int,
) ([]buildRun, error) {
	runs := make([]buildRun, len(extras))
	failures := make([]error, len(extras))
	notes := make([]bytes.Buffer, len(extras))
	// firstFailed is the earliest position in order of a build known to have failed, len(extras) while none has.
	var firstFailed atomic.Int64
	firstFailed.Store(int64(len(extras)))
	// running counts the builds admitted and not yet ended against limit, the width, one until it is decided, and
	// widthDecided is whether it is, which a fixed width is from the start.
	var admitLock sync.Mutex
	admitting := sync.NewCond(&admitLock)
	running, limit := 0, width
	widthDecided := measure == nil
	var deciding sync.Once
	// settle has limit be decided, unless a decision came first, and wakes every build waiting for it.
	settle := func(decided func() int) {
		deciding.Do(func() {
			decidedWidth := decided()
			admitLock.Lock()
			limit, widthDecided = decidedWidth, true
			admitting.Broadcast()
			admitLock.Unlock()
		})
	}
	// A width that needs no measuring is decided already, and the first build's end changes nothing of it, nor does it
	// under the share gate once the host's live heap decided.
	decide := func(loaded bool) {
		if measure == nil {
			return
		}
		settle(func() int {
			if loaded {
				return measure()
			}
			return 1
		})
	}
	// shares is the builds' share gate, nil but for a run that takes it (takesShareGate) whose width the memory
	// decides: without it the builds start as they are admitted, the first alone while measure decides.
	var shares *shareGate
	if measure != nil {
		limit = 1
	}
	if measure != nil && fromHostLive != nil {
		shares = newShareGate(len(order))
	}
	// judge is called once, by the last build to arrive at the share gate: a share over the line leaves the width to
	// measure, the first build's alone at the gate, and otherwise the host's live heap decides it now.
	judge := func() {
		admitLock.Lock()
		measured := shares.overLine()
		admitLock.Unlock()
		if !measured {
			settle(fromHostLive)
		}
		admitLock.Lock()
		shares.open(measured)
		admitting.Broadcast()
		admitLock.Unlock()
	}
	gate := newLintGate(lintBudget)
	var group sync.WaitGroup
	for position, index := range order {
		extra := extras[index]
		// Without the share gate a build is admitted as it starts; at the gate every build starts at once, since the
		// shares are judged only once every one has arrived, and is admitted at its gate.
		if shares == nil {
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
		}
		// Only the first build's load end can measure the width, and only one the memory decides.
		var afterLoad func(loaded bool)
		if position == 0 && measure != nil {
			afterLoad = decide
		}
		group.Go(func() {
			// admitted is read and written on this goroutine alone: the load calls its gate on the one it loads on.
			admitted := shares == nil
			var atGate func(fromSource int, packages int) error
			if shares != nil {
				atGate = func(fromSource int, packages int) error {
					// A reload for a question passes the gate again, and the build is admitted once.
					if admitted {
						return nil
					}
					admitLock.Lock()
					last := shares.report(position, fromSource, packages)
					admitLock.Unlock()
					if last {
						judge()
					}
					pipeline.shareGateStepped(extra, position, shareGateWaiting)
					admitLock.Lock()
					refused := false
					for {
						// A build after one known to have failed never type-checks, as one at a time it never would
						// start; a build before it is admitted all the same, as one at a time it would have run.
						if int64(position) > firstFailed.Load() {
							refused = true
							break
						}
						if shares.turn(position, widthDecided) && running < limit {
							break
						}
						if shares.judged {
							pipeline.shareGateStepped(extra, position, shareGateHeld)
						}
						admitting.Wait()
					}
					if refused {
						admitLock.Unlock()
						pipeline.shareGateStepped(extra, position, shareGateRefused)
						return errorRefusedAtGate
					}
					running++
					shares.admit(position)
					// Told under the lock, before the build after this one can pass, so a test reads the order builds
					// pass in from the order it was told.
					pipeline.shareGateStepped(extra, position, shareGateAdmitted)
					// The build after this one in order may be waiting only for this one to pass.
					admitting.Broadcast()
					admitLock.Unlock()
					admitted = true
					return nil
				}
			}
			// A build hands its slot back only once its failure is known, so at width 1 the next build is never
			// admitted after one that failed.
			defer func() {
				// A build that ended before its gate arrives there with no share, so the shares are still judged.
				if shares != nil {
					admitLock.Lock()
					last := shares.end(position)
					admitLock.Unlock()
					if last {
						judge()
					}
				}
				// The first build decides when it never reached its load's end, so the rest are never held. Only the
				// first: at the share gate a later build may end before the width is decided, and must not decide it.
				if position == 0 {
					decide(false)
				}
				admitLock.Lock()
				// A build that ended before it was admitted, at its gate or before it, never counted as running.
				if admitted {
					running--
				}
				admitting.Broadcast()
				admitLock.Unlock()
			}()
			runs[index], failures[index] = pipeline.extraBuildRun(extra, ours, host, gate, &notes[index], afterLoad,
				atGate)
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
	pipeline.buildsAtOnce = min(limit, max(len(extras), 1))
	admitLock.Unlock()
	for _, index := range order {
		// Ignored: a note that cannot reach stderr changes no record, as note says.
		_, _ = pipeline.standardError.Write(notes[index].Bytes())
		if failures[index] != nil {
			return nil, failures[index]
		}
	}
	return runs, nil
}

// shareGateStepped tells a test's shareGateSteps hook, when one is set, that extra, at position in start order,
// reached step at the share gate.
func (pipeline *pipeline) shareGateStepped(extra project.Build, position int, step shareGateStep) {
	if pipeline.shareGateSteps != nil {
		pipeline.shareGateSteps(extra, position, step)
	}
}

// buildsBeside is the host's build and the extra builds the census needs beside it, as describeProject read them
// (readBuilds), so the builds the lint phase checks are the ones the project record names a file checked by. A census
// that judged no file read none, and needs no extra build.
func (pipeline *pipeline) buildsBeside() (project.Build, []project.Build, error) {
	if pipeline.described.buildsError != nil {
		return project.Build{}, nil, pipeline.described.buildsError
	}
	if len(pipeline.described.builds) == 0 {
		return project.Build{}, nil, nil
	}
	return pipeline.described.builds[0], pipeline.described.builds[1:], nil
}

// recordsReadsOf is which packages the load and lint record what each read of other packages (#b2t9xmf), as
// load.Caching.RecordReads takes it. Every package, when the host keeps its packages for extra builds
// (keepsForBuilds). A run that does not records nothing, since it would pay for reads nothing reads.
func (pipeline *pipeline) recordsReadsOf() func(*packages.Package) bool {
	if pipeline.keepsForBuilds() {
		return load.EveryPackage
	}
	return nil
}

// keepsForBuilds is whether the host's run keeps its packages' entries for its extra builds to restore from (#b2t9xmf):
// only when the run checks extra builds and the census needs at least one.
func (pipeline *pipeline) keepsForBuilds() bool {
	_, extras, buildsError := pipeline.buildsBeside()
	return pipeline.request.checksEveryBuild() && buildsError == nil && len(extras) > 0
}

// readBuilds is the host's build, as go env's answer reads it, then the extra builds the census needs beside it
// (project.BuildsFor), with ports, the targets the toolchain lists (project.ReadPorts, read beside the census,
// earlyStart), and why each file no build compiles is left so, by path. targets are the settings' `targets`, each
// `GOOS/GOARCH` a port the toolchain lists or the run is refused: a target named and never checked would read as
// checked (#ntamp9e). Each file no build compiles is left to the types phase, which names it as a gap with that reason.
// reader hands BuildsFor each file's bytes as the description read them (heldReads).
func readBuilds(
	census project.Census,
	goEnvironment *load.GoEnvironment,
	ports []project.Port,
	targets []string,
	reader func(file string) ([]byte, error),
) ([]project.Build, map[string]string, error) {
	buildContext, goFlags := goEnvironment.Target()
	host := project.HostBuild(buildContext, goFlags)
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
	builds, unbuilt, buildsError := project.BuildsFor(census, host, ports, extraTargets, reader)
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
// unless the run renamed, whose build reads no cache, or measures under `--timing`, whose build has no session. Of
// those, the walk leaves untouched each whose dependence set misses what changed in the build (#qbvtkfm): restored
// unjudged and never analyzed, so the build has no say on its files and the host's findings stand there.
//
// A build restoring from host inherits too (#y6q1x2j): each of its syntax-only rules takes the host's findings on each
// file the host checked, rather than walk the file again in a package the build checks from source
// (check.HostInheritance), counted into the build's record.
//
// gate is the extra builds' lint gate, which the build takes its weight of once its first load has ended, by the share
// of its packages that load checked from source (lintWeight), and gives back once its rules end, every reload's
// included, on every path.
//
// notes is the build's own, where everything it notes goes rather than stderr, since builds side by side end in any
// order (extraBuildRuns).
//
// afterLoad, when not nil, is told as the build's first load ends whether it loaded, so the first build's holding can
// measure the width. atGate, when not nil, is the share gate the build waits at before each load's type check, every
// reload's included, whose error ends the load (load.Caching.AtGate). Whatever the width, the count the build's first
// load reached its gate with is its record's checkedFromSourceAtGate.
func (pipeline *pipeline) extraBuildRun(
	extra project.Build,
	ours map[string]bool,
	host *check.HostRun,
	gate *lintGate,
	notes io.Writer,
	afterLoad func(loaded bool),
	atGate func(fromSource int, packages int) error,
) (buildRun, error) {
	// A test's hold on the build before it reads anything (extraBuildStarts), nil in every run but a test's.
	if pipeline.extraBuildStarts != nil {
		pipeline.extraBuildStarts(extra)
	}
	// Under `--timeline` the build's stamps, each under the build's name, in its first round: its own steps, and its
	// first load's and check's. A reload's are in the reload's round, the rounds checkedFromSourceRounds counts. The
	// name is asked only of a run that stamps (buildTimeline).
	stamp := pipeline.buildTimeline(extra, 0)
	timeline.Tell(stamp, "start")
	environment := extraBuildEnvironment(pipeline.described.workspace, extra)
	// The build's one go env, which its findings cache's key and its load, the lister and the kept listing among it,
	// all read (#4rpt1jh), started with its lister state before the host's first load (earlyStart, startChildrenFirst)
	// and waited for here when it ran on past that (buildGoEnvironment.answer, #sfjj2rh), and read here only when that
	// failed.
	goEnvironment := pipeline.buildGoEnvironments[extra.String()].answer()
	if goEnvironment == nil {
		// Ignored: when go env fails the answer is nil and each asks on its own, as before, so the cache notes it sat
		// out and the load fails the build with go env's own words.
		goEnvironment, _ = load.ReadGoEnvironment(pipeline.described.workspace.Root, environment, nil,
			pipeline.described.children)
	}
	var session *check.CacheSession
	enabled, _ := pipeline.enabledRules()
	var inheritance *check.HostInheritance
	// As the host's lint does, a run that renamed reads no findings cache, whose keys are of the bytes it replaced.
	if !pipeline.renamed {
		var hostFiles cache.FileDigests
		if pipeline.session != nil {
			hostFiles = pipeline.session.Files()
		}
		// The build reads and writes the run's system cache in every run, one judging each build's dependence too
		// (#wzre5eg): its standard library is then loaded as a plain run's build loads it, restored where the system
		// cache holds it under the build's own key and checked from source and written there where it does not, so the
		// body reach's load mode rule compares the two loads a plain run has (bodyReacher.loadModeReaches). The facts
		// census compares no fact of a package the build restored, and counts each (compareFacts).
		session = pipeline.cacheSession(environment, goEnvironment, hostFiles, notes)
		// Each extra build reads one pack of each store rather than open every record it hits (#b2t9xmf); the host's
		// session keeps none.
		if session != nil {
			session.KeepBuildPacks(pipeline.described.workspace.Root)
		}
		if session != nil && !pipeline.keepsHostRestoreOff && !pipeline.censusKeepsHostRestoreOff {
			session.RestoreFromHost(host)
			if pipeline.keepsWalkOff {
				session.VisitEvery()
			}
			if pipeline.declinedAtLoad != nil {
				session.TellDeclinesAtLoad(func(identifier string, why string) {
					pipeline.declinedAtLoad(extra, identifier, why)
				})
			}
			if pipeline.judgedSkipped != nil {
				session.JudgeSkipped(func(identifier string) {
					if pipeline.skippedAtLoad != nil {
						pipeline.skippedAtLoad(extra, identifier)
					}
				}, func(identifier string, judgment check.SkippedJudgment, why string) {
					pipeline.judgedSkipped(extra, identifier, judgment, why)
				})
				if pipeline.forcedThrough != "" {
					session.ForceThrough(pipeline.forcedThrough)
				}
			}
			// Only a build restoring from the host's run inherits from it: the same run read the same bytes for both.
			if !pipeline.keepsInheritanceOff {
				inheritance = host.Inheritance(enabled)
			}
		}
	}
	// As the host's load does (loadWorkspace), a package with no file in scope is loaded and never analyzed. A root no
	// action reaches, a command main whose test variant is analyzed in its place, is left out of the build's load
	// (#ws2ptvf): the host checked it, and its variant carries its files here. The listing reads through the host's
	// directory reads (#yyvstj6), its kept answer's fingerprint through the builds' own (#5jpss95), and its patterns'
	// walks through the ones read since the run last changed the bytes it holds, the host's last load's but after a
	// rename (#tb538na, #vnb7btx).
	caching := load.Caching{
		Held:              pipeline.held,
		GoEnvironment:     goEnvironment,
		Timing:            pipeline.timing,
		Analyzes:          pipeline.described.hasFileInScope,
		AnalyzedRootsOnly: true,
		SharedReads:       pipeline.reads,
		Fingerprints:      pipeline.fingerprints,
		Walks:             pipeline.walks,
		// Under `--timeline` the build's load stamps its listing's end, its restore and its first parse.
		Timeline: stamp,
	}
	if session != nil {
		caching.Restorer = session
		if session.HasStore() {
			caching.Listings = session
		}
	}
	// The first load's count at its gate, which a reload's never replaces, as checkedFromSource's first round is the
	// first load's: the gauge reads the gate's share beside the final one.
	checkedAtGate, reachedGate := 0, false
	// loading is every load's wall time but its wait at the share gate, which is shareGateWait's: a build held there
	// waits for the builds before it, which main's schedule never started it beside, so its load time stays its own.
	// The wait is taken off here, inside the load, and the load's whole time added once it returns.
	var loading time.Duration
	var shareGateWait time.Duration
	// loadWall is every load's whole wall time, its wait at the share gate included, which only a test's buildLoadTimes
	// hook reads, to check the two figures the record splits it into.
	var loadWall time.Duration
	caching.AtGate = func(fromSource int, packages int) error {
		if !reachedGate {
			checkedAtGate, reachedGate = fromSource, true
		}
		if atGate == nil {
			return nil
		}
		waitStarted := time.Now()
		gateError := atGate(fromSource, packages)
		waited := time.Since(waitStarted)
		loading -= waited
		shareGateWait += waited
		return gateError
	}
	// A plain run's must-check census judges the build at its restore, from its first load's listing alone, once:
	// a reload's listing names the same files (#anfjtr3). Listed runs on the load's goroutine, each load in turn, so
	// the set is read only once the load has returned.
	var listedMustCheck *mustCheckSet
	if pipeline.plainBits != nil {
		caching.Listed = func(reachable []*packages.Package) {
			if listedMustCheck == nil {
				listedMustCheck = pipeline.plainMustCheckOf(reachable)
			}
		}
	}
	var linting time.Duration
	if pipeline.extraBuildLoads != nil {
		pipeline.extraBuildLoads(extra)
	}
	started := time.Now()
	loaded, loadError := pipeline.loadPackages(loadRequest{environment: environment, caching: &caching})
	elapsed := time.Since(started)
	loading += elapsed
	loadWall += elapsed
	timeline.Tell(stamp, "load.end")
	// The first build's load has ended and its rules have not started: it holds none of the run's slots, and every
	// other build waits at the share gate, before its type check, so the width is measured here, before any other
	// build is admitted (extraBuildRuns). Their listings, graphs and restores are live as it measures, so the holding
	// reads high, which errs narrow.
	if afterLoad != nil {
		afterLoad(loadError == nil)
	}
	if loadError != nil {
		return buildRun{}, fmt.Errorf("loading the build %s: %w", extra, loadError)
	}
	var heldAtLoadEnd uint64
	if pipeline.timing != nil {
		heldAtLoadEnd = holdingAbove(forcedLiveHeapBytes(), pipeline.timedHostLiveBytes)
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
		// The first load's wait, the only one: a reload passes the gate at once.
		shareGateWait: shareGateWait,
		// What it held as its load ended is the build's own under `--timing` with any load: a load with type errors
		// holds its packages too.
		heldAtLoadEnd: heldAtLoadEnd,
	}
	if session != nil {
		run.files = session.Files()
	}
	// Judged on the host's graph from this first load alone, which a reload for a question never changes the files of.
	run.dependence, run.judged = pipeline.dependenceOf(loaded)
	// The build's must-check set for the census: the whole graph's under `--build-dependence`, judged after this load,
	// or the plain bits', judged at its listing (#anfjtr3). Either is nil without the census, so a plain run never
	// takes the lock.
	mustCheck := listedMustCheck
	if run.judged != nil && run.judged.mustCheck != nil {
		mustCheck = run.judged.mustCheck
	}
	if mustCheck != nil {
		pipeline.keepMustCheckSet(extra, mustCheck)
	}
	if len(loaded.Diagnostics) > 0 {
		return run, nil
	}
	run.packagesLoaded = loaded.PackagesLoaded
	run.checkedFromSourceAtGate = checkedAtGate
	// The first load's count weighs the lint at the gate; every load's, a reload's included, reaches the record.
	run.checkedFromSource = checkedFromSource(loaded.Roots)
	run.checkedFromSourceRounds = []int{run.checkedFromSource}
	weight := lintWeight(run.checkedFromSource, run.packagesLoaded)
	pipeline.lintStepped(extra, lintStepReady, weight)
	waitStarted := time.Now()
	run.lintWaited = gate.take(weight, func() { pipeline.lintStepped(extra, lintStepQueued, weight) })
	// A build admitted at once waited for nothing, which the record writes by leaving the wait out.
	if run.lintWaited {
		run.lintWait = time.Since(waitStarted)
	}
	pipeline.lintStepped(extra, lintStepTaken, weight)
	timeline.Tell(stamp, "gate.taken")
	// The build's standing bodies, made once for every load of it, only where the run leaves bodies standing, and read
	// by a load only where its passes are handed the host's sides.
	var standing *buildStanding
	if pipeline.standingConfined != nil {
		standing = newBuildStanding(host, enabled, pipeline.standingConfined)
	}
	// A nil *check.HostInheritance would be a non-nil rule.Inheritance, which every pass would read as one.
	var inherits rule.Inheritance
	if inheritance != nil {
		inherits = inheritance
	}
	runRules := func(over *load.Loaded) (*check.Result, error) {
		// The body reach census of the build's last load, and its bodies the reach leaves unreached compared with the
		// host's, taken before its rules let its syntax and types go, under the census alone. On the build's own
		// goroutine, before the facts census, so its judgment is asked from one goroutine at a time.
		if pipeline.bodyReachFromSourceRecord != "" {
			run.fromSourceIdentifiers = fromSourceIdentifiers(over)
		}
		if pipeline.bodyReachHost != nil && run.judged != nil {
			built := reachBuiltPackages(over, pipeline.dependence.files)
			graph := newReachBuiltGraph(over)
			sizesDiffer := buildSizesDiffer(pipeline.dependence.from, over)
			run.bodyReach = pipeline.bodyReachHost.reach(run.judged.dependence, built, graph, sizesDiffer)
			// The same census again over the packages a plain run of the build checks from source, beside the whole.
			if recorded := pipeline.recordedFromSource(extra); recorded != nil {
				run.bodyReachFromSource = pipeline.bodyReachHost.reach(run.judged.dependence,
					slices.DeleteFunc(slices.Clone(built), func(read *reachBuiltPackage) bool {
						return !recorded[read.identifier]
					}), graph, sizesDiffer)
			}
			if pipeline.bodyDifferential {
				census := run.bodyReach
				if pipeline.bodyDifferentialCensus != nil {
					census = pipeline.bodyDifferentialCensus(census)
				}
				run.bodyDifferential = newBuildBodyDifferential(pipeline.bodyReachHost, over, census)
			}
		}
		// Each pass is handed the host's side of its package for the facts' reach, with the bodies this load's census
		// leaves unreached standing, read by the actions of the run at once and written by none.
		var factHosts func(packageId string) fact_reach.Host
		if pipeline.factReachSides != nil && run.bodyReach != nil {
			hosts := pipeline.factReachSides.inBuild(run.bodyReach)
			run.factHosts = hosts
			factHosts = func(packageId string) fact_reach.Host { return hosts[packageId] }
		}
		settle := pipeline.factReachSettle(factHosts != nil)
		// A package whose pass is handed a host's side lints only what does not stand once standing_facts has settled
		// it there, whose action every dispatcher's action on the package follows, and the representation's build too,
		// which then leaves unbuilt what stands for every rule its dispatcher starts.
		var standingBodies func(packageId string, after any) rule.StandingBodies
		var standingAfter *analysis.Analyzer
		if standing != nil && settle != nil {
			standingBodies, standingAfter = standing.bodiesOf, settle
		}
		started := time.Now()
		defer func() { linting += time.Since(started) }()
		return check.Run(over.Packages, enabled, check.Options{
			SingleThreaded: pipeline.request.switches["single-threaded"],
			Pool:           pipeline.workPool(),
			Cache:          session,
			Withheld:       over.Withheld,
			Bodiless:       over.Bodiless,
			// Nothing reads the build's syntax after its rules: the merge reads only what the run hands it.
			ReleaseAnalyzed: true,
			ReadFile:        pipeline.sourceReader(nil, over.Rewritten),
			Timing:          pipeline.timing,
			// Under `--build-dependence` the build's facts are compared with the host's (#0ghk7vj).
			ExportFacts: pipeline.dependence != nil,
			Inheritance: inherits,
			FactHosts:   factHosts,
			// The standing bodies each pass's side leaves are settled, for the census to count, and under the body
			// differential the build's own facts are captured, for its second leg to compare those left standing by.
			FactReachSettle: settle,
			// And no presence reader (FactReachPresence): the second leg, the capture's one reader, compares facts
			// alone (compareStanding), so only the host's run reads its bodies' presence, for the sides it hands out.
			FactReach: pipeline.bodyDifferential && factHosts != nil,
			// Under `--timeline` the check stamps in the round of the load it runs over, the last one counted.
			Timeline:       pipeline.buildTimeline(extra, len(run.checkedFromSourceRounds)-1),
			StandingBodies: standingBodies,
			StandingAfter:  standingAfter,
			// U1000 follows standing_facts too, so it splices the host's record of a standing body in place of
			// walking it (#xeafx79): check takes it and never imports it (#f5g7ym6).
			StandingFollowers: []*analysis.Analyzer{unused_code.Analyzer},
		})
	}
	// The rules and every reload run within one function whose one deferred return of the weight is the only one, so
	// a build that fails while holding weight gives it back by the same line as one that succeeds, and a test that
	// shows the weight comes back covers both: a leak on any path would leave every build behind it waiting for good.
	// It comes back before the build's end, which a test may hold until other builds have ended, and they may be
	// waiting for this weight.
	var stopSampling func() uint64
	if pipeline.timing != nil {
		stopSampling = heldWhileLinting(lintPeakSampleInterval)
	}
	result, lintError := func() (*check.Result, error) {
		defer func() {
			pipeline.lintStepped(extra, lintStepGiving, weight)
			gate.giveBack(weight)
		}()
		result, checkError := runRules(loaded)
		// As the host's lint does (reloadedPass), a question that reached a package the cache restored, with no
		// answer stored, has the build loaded again with that package checked from source, until none does. So does a
		// package restored from the host's run whose facts or answers do not hold here (check.Result.Reloads).
		for checkError == nil && len(result.Reloads) > 0 && session != nil && session.Reload(result.Reloads) {
			reloads := strings.Join(result.Reloads, ", ")
			// The reload's load, its end too, and the check over it, stamp in the round this load is of: 1 for the
			// first reload.
			caching.Timeline = pipeline.buildTimeline(extra, len(run.checkedFromSourceRounds))
			if pipeline.extraBuildLoads != nil {
				pipeline.extraBuildLoads(extra)
			}
			started := time.Now()
			loaded, loadError = pipeline.loadPackages(loadRequest{environment: environment, caching: &caching})
			elapsed := time.Since(started)
			loading += elapsed
			loadWall += elapsed
			timeline.Tell(caching.Timeline, "load.end")
			if loadError != nil {
				return nil, fmt.Errorf("loading the build %s again to check %s from source: %w", extra, reloads,
					loadError)
			}
			if len(loaded.Diagnostics) > 0 {
				return nil, fmt.Errorf("loading the build %s again to check %s from source found %d type "+
					"errors the first load did not", extra, reloads, len(loaded.Diagnostics))
			}
			run.checkedFromSourceRounds = append(run.checkedFromSourceRounds, checkedFromSource(loaded.Roots))
			result, checkError = runRules(loaded)
		}
		if checkError != nil {
			return nil, fmt.Errorf("running the rules over the build %s: %w", extra, checkError)
		}
		return result, nil
	}()
	if stopSampling != nil {
		run.heldAtLintPeak = holdingAbove(max(stopSampling(), heldAtLoadEnd+pipeline.timedHostLiveBytes),
			pipeline.timedHostLiveBytes)
	}
	if lintError != nil {
		return buildRun{}, lintError
	}
	for _, problem := range result.Cache.Problems {
		noteTo(notes, "the findings cache of the build %s: %s", extra, problem)
	}
	// What the build analyzed, and so has a say on, is the last load's, whose result stands: a load again for a
	// question can check from source a package an earlier load's walk left untouched.
	for _, each := range loaded.Packages {
		run.analyzed[each.ID] = true
	}
	run.said = compiledFiles(loaded.Packages, ours)
	run.untouchedFiles = compiledFiles(loaded.Untouched, ours)
	run.result = result
	pipeline.systemCache.failedAt(session)
	// The facts census reads the last load's facts, whose result stands, beside the host's (#0ghk7vj).
	if run.judged != nil && run.dependence != nil {
		run.dependence.Facts = pipeline.compareFacts(run.judged, result.Facts)
	}
	// The facts' reach the last load's passes settled, whose result stands, and each package they failed to settle on,
	// noted under the build's name.
	if result.FactReachUnsettled != "" {
		run.factReach = &buildFactReach{unsettled: result.FactReachUnsettled}
	}
	if result.FactReachSettled != nil {
		run.factReach = newBuildFactReach(run.factHosts, result)
		for _, crash := range result.FactReachCrashes {
			noteTo(notes, "the facts' reach of the build %s could not settle %s: %s", extra, crash.File, crash.Error)
		}
	}
	// The findings of each body the differential compared, the last load's, whose result stands, a difference classed
	// under a later stage only where the judgment shows its cause (findingCauses) and a body the facts' reach took out
	// left to the build, and its second leg, over the facts of each body the facts' reach left standing, whose capture
	// goes once it is read: its no-return predicates hold the build's resolver's packages. A differential is made only
	// of a judged build (runRules).
	if run.bodyDifferential != nil {
		run.bodyDifferential.compareFindings(pipeline.bodyReachHost.findings,
			findingsByPackage(result.Findings, loaded.Packages),
			findingStagesOf(enabled), &findingCauses{dependence: run.judged.dependence,
				filesDiffer: run.judged.filesDiffer}, pipeline.factReachSides, result.FactReachSettled)
		if result.FactReach != nil && result.FactReachSettled != nil {
			built := make(map[string]*packages.Package, len(loaded.Packages))
			for _, each := range loaded.Packages {
				built[each.ID] = each
			}
			run.bodyDifferential.compareStanding(extra.String(), pipeline.factReachSides, result.FactReachSettled,
				result.FactReach, built)
		}
		result.FactReach = nil
	}
	run.loading, run.linting, run.shareGateWait = loading, linting, shareGateWait
	if pipeline.buildLoadTimes != nil {
		pipeline.buildLoadTimes(extra, loadWall, shareGateWait)
	}
	run.inheritedFiles, run.inheritedFindings = inheritance.Counts()
	run.standingDeclarations, run.standingFindings = standing.counts()
	if pipeline.extraBuildEnds != nil {
		if endError := pipeline.extraBuildEnds(extra, notes); endError != nil {
			return buildRun{}, fmt.Errorf("the build %s: %w", extra, endError)
		}
	}
	timeline.Tell(stamp, "end")
	return run, nil
}

// buildTimeline is extra's timeline hook in round under `--timeline` (timeline.Recorder's Hook), and nil for a run
// that did not ask. The build reaches the recorder's interface only past the nil check, so a run without the switch
// copies no build into an interface for it, and asks no name of it.
func (pipeline *pipeline) buildTimeline(extra project.Build, round int) func(label string) {
	if pipeline.request.timeline == nil {
		return nil
	}
	return pipeline.request.timeline.Hook(extra, round, "")
}

// wholeReads is result's reads once every type-read walk the run left running has ended (check.Result.ReadWalks):
// read any earlier, a package whose walk is still running reads as unknown.
func wholeReads(result *check.Result) map[*packages.Package]*build_reads.Reads {
	result.ReadWalks.Wait()
	return result.Reads
}

// lintStepped tells a test's lintSteps hook, when one is set, that extra's lint reached step holding or asking weight.
func (pipeline *pipeline) lintStepped(extra project.Build, step lintStep, weight int) {
	if pipeline.lintSteps != nil {
		pipeline.lintSteps(extra, step, weight)
	}
}

// otherBuildsFailed notes why the builds beside the host's were not checked, which leaves the run incomplete: it
// was asked for them, and checked only the host's.
func (pipeline *pipeline) otherBuildsFailed(cause error) {
	pipeline.note("the builds beside the host's were not checked: %v", cause)
	pipeline.incomplete = true
}

// oursSet is every readable file of ours, as the census and the load account for them.
func (pipeline *pipeline) oursSet() map[string]bool {
	ours := make(map[string]bool, len(pipeline.described.readableOurs))
	for _, file := range pipeline.described.readableOurs {
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
func (pipeline *pipeline) mergeBuildRuns(runs []buildRun, ran []string) *buildsVerdict {
	pipeline.buildRuns = runs
	outcomes := make([]buildOutcome, 0, len(runs))
	for _, run := range runs {
		outcomes = append(outcomes, pipeline.outcomeOf(run, ran))
	}
	verdict := &buildsVerdict{merged: mergeBuilds(outcomes, mergeIfAllRules(pipeline.registered))}
	for index, run := range runs {
		record := contract.BuildRecord{
			Target:           run.build.String(),
			Host:             index == 0,
			CompiledFiles:    len(run.compiled),
			AnalyzedPackages: len(run.analyzed),
			FindingsAdded:    verdict.merged.added[index],
			FindingsDropped:  verdict.merged.dropped[index],
			// An extra build's alone: the host's run counts neither, so its record writes neither.
			CheckedFromSource: sumOf(run.checkedFromSourceRounds),
			// Each load's count, written only for a build loaded more than once.
			CheckedFromSourceRounds: roundsIfReloaded(run.checkedFromSourceRounds),
			PackagesLoaded:          run.packagesLoaded,
			CheckedFromSourceAtGate: run.checkedFromSourceAtGate,
			// An extra build's alone, and under `--timing` alone: 0 otherwise, which the record leaves out.
			HeldAtLoadEndBytes:  run.heldAtLoadEnd,
			HeldAtLintPeakBytes: run.heldAtLintPeak,
			LintPeakOverLoadEnd: lintPeakOverLoadEnd(run.heldAtLoadEnd, run.heldAtLintPeak),
			// An extra build's alone, under `--build-dependence`: nil otherwise, which the record leaves out.
			Dependence: run.dependence,
			// An extra build's alone: 0 otherwise, which the record leaves out.
			InheritedFiles:       run.inheritedFiles,
			InheritedFindings:    run.inheritedFindings,
			StandingDeclarations: run.standingDeclarations,
			StandingFindings:     run.standingFindings,
		}
		recordTimes(&record, run)
		if run.result != nil {
			record.EntriesKept = run.result.Host.EntriesKept()
			fromHost := run.result.FromHost
			record.RestoredFromHost = len(fromHost.Restored)
			record.Untouched, record.Visited = len(fromHost.Untouched), fromHost.Visited
			record.WrittenThrough = run.result.Cache.WrittenThrough
			record.SystemCacheRestored = run.result.Cache.FromSystemCache
			record.SystemCacheWritten = run.result.Cache.SystemCacheWritten
			record.SharedBodyHits = run.result.Cache.SharedBodyHits
			record.Packs = summedPacks(nil, run.result.Cache.Packs)
			record.DeclinedAtLoad = fromHost.DeclinedAtLoad
			record.DeclinedAtCheck = fromHost.DeclinedAtCheck
			if index > 0 && os.Getenv(renderingCountsVariable) == "1" {
				// Ignored: a census that cannot reach stderr changes nothing of the run.
				_, _ = fmt.Fprintf(pipeline.standardError, "cohere: %s rendering: %d nodes reused, %d rendered, %d of "+
					"them unmatched\n", run.build.String(), fromHost.NodesReused, fromHost.NodesRendered,
					fromHost.NodesUnmatched)
			}
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
	pipeline.recordBuildCoverage(runs, verdict.records)
	return verdict
}

// summedPacks is sum, a record's count of what extra builds did with their packs, with counted, one build's, added,
// nil while both are, for a session that kept no packs. sum is never written: a new count is returned.
func summedPacks(sum *contract.BuildPacks, counted *check.PackCounts) *contract.BuildPacks {
	if counted == nil {
		return sum
	}
	summed := contract.BuildPacks{}
	if sum != nil {
		summed = *sum
		summed.RefusedBy = maps.Clone(sum.RefusedBy)
	}
	summed.Read += counted.Read
	summed.Missing += counted.Missing
	summed.RecordsServed += counted.RecordsServed
	summed.RecordsStale += counted.RecordsStale
	summed.RecordsMissed += counted.RecordsMissed
	summed.ShardReads += counted.ShardReads
	summed.Written += counted.Written
	for word, count := range counted.RefusedBy {
		if summed.RefusedBy == nil {
			summed.RefusedBy = map[string]int{}
		}
		summed.RefusedBy[word] += count
	}
	return &summed
}

// checkedFromSource is how many packages of roots' whole graph a load checked from source. A package the load checked
// from source has type maps, which the loader's check sets first on every package it checks, while one whose types
// came from export data, the build's own findings cache's or the host's run's, or that the load never needed, has
// none: check.Run's walks of type reads tell the two apart the same way. Its syntax would not, since a package checked
// from source none of whose files parsed has none either.
func checkedFromSource(roots []*packages.Package) int {
	count := 0
	packages.Visit(roots, nil, func(each *packages.Package) {
		if each.TypesInfo != nil {
			count++
		}
	})
	return count
}

// roundsIfReloaded is rounds when there are more than one, and nil for a build loaded once, whose one round is its sum.
func roundsIfReloaded(rounds []int) []int {
	if len(rounds) < 2 {
		return nil
	}
	return rounds
}

// sumOf is the sum of counts.
func sumOf(counts []int) int {
	sum := 0
	for _, count := range counts {
		sum += count
	}
	return sum
}

// recordTimes writes into record where run's time went: its load and its lint, every reload's included, the host's wait
// for its walks before the extra builds could start, an extra build's wait at the share gate, which its load's time
// leaves out, and its lint's wait for its weight at the lint gate, and of
// its result, an extra build's deciding which packages restore from the host's run and the host's keeping its packages
// for the others, each in wall milliseconds, as the record writes them, and whether the build's lint waited at all.
//
// Mutates: record gains run's times
func recordTimes(record *contract.BuildRecord, run buildRun) {
	record.LoadMilliseconds = int(run.loading.Milliseconds())
	record.LintMilliseconds = int(run.linting.Milliseconds())
	record.WaitMilliseconds = int(run.waited.Milliseconds())
	record.ShareGateWaitMilliseconds = int(run.shareGateWait.Milliseconds())
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
func (pipeline *pipeline) recordBuildCoverage(runs []buildRun, records []contract.BuildRecord) {
	inScope := func(file string) bool {
		return pipeline.described.inScopeSet[file] || pipeline.described.constrainedInScope[file]
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
		if !linted && pipeline.described.constrainedInScope[file] {
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
