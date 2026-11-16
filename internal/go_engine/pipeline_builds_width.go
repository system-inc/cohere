package go_engine

import (
	"fmt"
	"math"
	"runtime"
	"runtime/metrics"
	"slices"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// liveHeapMetric is the runtime's live heap at its last collection, read without forcing one, which the runtime sets
// as a collection's mark terminates.
const liveHeapMetric = "/gc/heap/live:bytes"

// forcedCyclesMetric counts the collections runtime.GC forced, which the runtime moves as a forced collection's mark
// terminates, after it set the live heap of that collection and before any goroutine runs again (gcMarkTermination).
const forcedCyclesMetric = "/gc/cycles/forced:gc-cycles"

// markPollInterval is how often forcedLiveHeapBytes asks whether its collection's mark has terminated, a sleep between
// reads so the wait never spins a core while the mark runs: a mark at cohere's root takes tens of milliseconds, and
// the sweep after it, which the width does not wait for, most of the rest of the 0.2 s runtime.GC takes (#geaqd90).
const markPollInterval = time.Millisecond

// fixedBuildsWidth is how many of the run's extras extra builds run side by side when no heap need be measured for it,
// and true then, decided once as they start (#b2t9xmf): one at a time unless the builds would restore packages from
// host, the host's run, as a plain run's do (wouldRestoreFromHost), a test's forced width when it set one, one at a
// time for the release's standard-library child, and one at a time when no heap could make it more (buildsWidth over
// the smallest heap). It is false when the width is the memory's to decide at the share gate (shareGate), by the
// host's live heap (hostLiveBuildsWidth) or by what the first build holds (measuredBuildsWidth). The width never
// reaches a key or the verdict, only the lint record's buildsAtOnce and the heap it was decided by: the builds'
// results and notes are taken in one order whatever it is (extraBuildRuns).
//
// Builds that restore nothing each check every package, about what the host's did: two of those side by side under
// the cap drive the collector, and at cohere's root made the run 6 to 8 s slower and cost about 70 user-s more, while
// builds that restore most of their packages from the host ran 22% faster side by side. A run judging each build's
// dependence restores nothing for its facts census alone and keeps a plain run's width (#f9br7sg): warm, its builds
// each hit their own store, and cold, each build that checks most of its packages from source lints alone at the gate
// (lintGate), so only their loads overlap.
func (engine *pipeline) fixedBuildsWidth(extras int, host *check.HostRun) (int, bool) {
	if !engine.buildsWidthUnguarded && !engine.wouldRestoreFromHost(host) {
		return 1, true
	}
	if engine.buildsWidthForced > 0 {
		return min(engine.buildsWidthForced, max(extras, 1)), true
	}
	// The release's standard-library child collects every entry its sessions save into one map, which each session
	// writes under a lock of its own, so two builds saving at once would write it together. That child checks no
	// extra build today; one at a time keeps it safe if it ever does.
	if engine.request.standardLibrary.collected != nil {
		return 1, true
	}
	// A heap of one byte held by each build is the least any can be, so the widest the builds and the limit allow:
	// when even that is one at a time, no heap can widen it, and the run pays no collection to measure one.
	if width, _ := buildsWidth(extras, engine.request.switches["single-threaded"], engine.memory, 1, 1,
		engine.withFindingsStore()); width == 1 {
		return 1, true
	}
	return 0, false
}

// lowShareAtGate is the largest share of its packages a build may report at the share gate, checked from source over
// held, for the width to be decided from the host's live heap alone (shareGate, hostLiveBuildsWidth): when every
// build's share is at or under it, each is taken to hold about what the host does (#ghb3rcn).
//
// It is a heuristic, not a proof. Nearly every restore is decided late (load.Restored.Holds), so the share the gate
// reads is a lower bound: on the platform builds the upper bound, every late restore declined, read 0.96 to 0.97 at
// cohere's root and deeafbf4, and the lower bound read 2 times under the final share there and 3.7 times under at loom
// (0.153 against 0.56). A wrong call costs memory over a soft limit and a slower run, never a finding. The known limit
// is a gate share under the line whose final share is high (#bg2ffdw), which a build's record shows beside its
// final share (checkedFromSourceAtGate, checkedFromSource, packagesLoaded).
const lowShareAtGate = 0.10

// buildHoldingBudget is how many times the holding a build is taken to hold as its load ends the width budgets for
// it, before the peak factor (buildPeakOverLoadEnd): the host's live heap when every share is low
// (hostLiveBuildsWidth), and the first build's measured holding otherwise (measuredBuildsWidth). It is fitted to one
// tree's best width, cohere's root's 6 against 7, not a holding anyone measured: the heaviest low-share build there,
// windows, holds about 1.3 to 1.4 times the host's live heap, and linux 0.95 (#bg2ffdw). 1.2, with the peak factor's
// own margin, is the narrowest value that kept the root's best width. A row at 1.4 is owed and may move it.
const buildHoldingBudget = 1.2

// budgetedHolding is holding times buildHoldingBudget, rounded up to the byte, what the width budgets each build.
func budgetedHolding(holding uint64) uint64 {
	return uint64(math.Ceil(float64(holding) * buildHoldingBudget))
}

// hostLiveBuildsWidth is how many of extras extra builds run side by side when every build's share at the gate is low
// (lowShareAtGate): each build is budgeted at the host's live heap, hostLive, measured by a collection as the builds
// started, times buildHoldingBudget, and nothing is measured again (#ghb3rcn). It returns the width, whether the slack
// added its last build (buildsWidth), and the budgeted holding.
func (engine *pipeline) hostLiveBuildsWidth(extras int, hostLive uint64) (int, bool, uint64) {
	holding := budgetedHolding(hostLive)
	width, slack := buildsWidth(extras, engine.request.switches["single-threaded"], engine.memory, hostLive, holding,
		engine.withFindingsStore())
	return width, slack, holding
}

// measuredBuildsWidth is how many of extras extra builds run side by side by what the first build to start holds
// (#rmzehfg), the heaviest the run has (heaviestFirst), when a share at the gate is over lowShareAtGate: the live heap
// with that build's load ended, measured by a collection run for it (forcedLiveHeapBytes), less hostLive, the host's
// live heap measured the same way as the builds started, times buildHoldingBudget. It returns the width and the
// budgeted holding, at least one byte. The first build passes the share gate alone and calls it between its load and
// its rules, holding none of the run's slots, and the rest pass once it is decided (extraBuildRuns). The other builds'
// listings, graphs and restores are live as it measures, since they wait at the gate, so the holding reads high,
// which errs narrow. Beside the width it returns whether the slack added its last build (buildsWidth).
func (engine *pipeline) measuredBuildsWidth(extras int, hostLive uint64) (int, bool, uint64) {
	holding := budgetedHolding(holdingAbove(forcedLiveHeapBytes(), hostLive))
	width, slack := buildsWidth(extras, engine.request.switches["single-threaded"], engine.memory, hostLive, holding,
		engine.withFindingsStore())
	return width, slack, holding
}

// holdingAbove is what a build holds of live, the live heap measured with it loaded, beyond hostLive, the host's
// measured as the builds started, at least one byte, so a build whose heap the host's collection outweighed still
// holds something.
func holdingAbove(live uint64, hostLive uint64) uint64 {
	if live > hostLive {
		return live - hostLive
	}
	return 1
}

// heaviestFirst is the order the extra builds start in, as indexes of extras: the first build on a platform other than
// host's, alone, then every other build in build order, so the first to start, the one the width is measured by
// (measuredBuildsWidth), is among the heaviest. A build on another platform holds the most, since the standard
// library's types differ there and its packages restore least from the host's run: on Nov 11 the platform builds of
// cohere's root held 0.90 to 1.17 GB beyond the host's heap as their loads ended, and its tag builds, which restore
// nearly every package, 0.13 to 0.14 GB (#rmzehfg).
//
// Only the first goes ahead of build order, which puts the host's own builds, the light ones, before the other
// platforms: starting every platform build first put linux, windows and freebsd side by side at cohere's root, their
// lint peaks coincided, the collector's mark doubled, and `--no-cache` every build ran 11.4 to 13.7 s at width 4
// against 9.0 to 10.3 s in build order (#awbzy9y). A run with no build on another platform starts in build order. The
// merge takes the runs in build order whatever order they started in, and stderr their notes in this one
// (extraBuildRuns).
func heaviestFirst(host project.Build, extras []project.Build) []int {
	heaviest := slices.IndexFunc(extras, func(extra project.Build) bool {
		return extra.OperatingSystem != host.OperatingSystem || extra.Architecture != host.Architecture
	})
	order := make([]int, 0, len(extras))
	if heaviest >= 0 {
		order = append(order, heaviest)
	}
	for index := range extras {
		if index != heaviest {
			order = append(order, index)
		}
	}
	return order
}

// wouldRestoreFromHost reports whether the extra builds would restore packages from host, the host's run, as a plain
// run's do: it kept entries for them, and nothing keeps the builds from reading them, a test turning the restore off or
// a run whose rename wrote, whose builds read no findings cache. A run under `--timing` keeps no entries. The facts
// census's restore off (censusKeepsHostRestoreOff) is the census's alone and no reason here (#f9br7sg): a run judging
// each build's dependence still keeps the host's entries, and its builds run at the width a plain run's would.
func (engine *pipeline) wouldRestoreFromHost(host *check.HostRun) bool {
	return host.EntriesKept() > 0 && !engine.keepsHostRestoreOff && !engine.renamed
}

// buildPeakOverLoadEndWithStore and buildPeakOverLoadEndWithoutStore are how many times what a build holds as its load
// ends it is taken to hold at its lint's peak, when the rules have built each package's intermediate representation
// over the types its load holds, in a run with a findings store and in one without (`--no-cache`): the width is sized
// by that peak, since what builds hold while they lint side by side is what drove the collector (#rmzehfg). A build's
// load end is a fixed point the width can be measured at, which its peak is not. The two modes are measured apart
// because a load without a store holds more at its end for the same peak, so one factor for both narrowed `--no-cache`,
// the bar's own path, to width 2 at cohere's root where main had run 4 and 1.4 s faster (#hv090f4).
//
// Each is the largest ratio measured in its mode, rounded up to the next half. On Nov 11, the 14 extra builds of
// cohere's root and deeafbf4 were each run alone, their live heap taken by a forced collection at the load's end and
// sampled through the lint: with the findings store, 2.63 to 3.22 at the root and 1.89 to 2.57 at deeafbf4; under
// `--no-cache`, 1.31 to 2.09 at the root and 1.12 to 2.16 at deeafbf4. A tree whose ratio passes its factor shows in
// one run under `--timing`, whose build records carry each build's holding at both points and their ratio, and a
// constant changes only on such a record. buildPeakOverLoadEndMeasuredWithStore and
// buildPeakOverLoadEndMeasuredWithoutStore are those largest ratios before the rounding, 3.22 and 2.16: the bound the
// width's slack spends to (buildsWidth), and the bound a timed run checks each build's ratio against
// (recordBuildsPeakProblems), so a tree that passes one is named the first time it is measured.
const (
	buildPeakOverLoadEndWithStore            = 3.5
	buildPeakOverLoadEndWithoutStore         = 2.5
	buildPeakOverLoadEndMeasuredWithStore    = 3.22
	buildPeakOverLoadEndMeasuredWithoutStore = 2.16
)

// buildsWidthSlackMarginBytes is what the width's slack leaves free under the cap beyond every build at the largest
// ratio measured in its mode (buildsWidth): 512 MiB, which at deeafbf4, three builds each holding about 1.07 GB as
// their loads end, leaves room for each to pass the largest ratio measured by about 0.16 (#ewkp7w0).
const buildsWidthSlackMarginBytes = 512 << 20

// buildPeakOverLoadEnd is the factor for a run with a findings store when withStore is true, and for one without.
func buildPeakOverLoadEnd(withStore bool) float64 {
	if withStore {
		return buildPeakOverLoadEndWithStore
	}
	return buildPeakOverLoadEndWithoutStore
}

// buildPeakOverLoadEndMeasured is the largest ratio measured in a run with a findings store when withStore is true,
// and in one without, with the constant's name, which a problem with a build's ratio names (recordBuildsPeakProblems).
func buildPeakOverLoadEndMeasured(withStore bool) (float64, string) {
	if withStore {
		return buildPeakOverLoadEndMeasuredWithStore, "buildPeakOverLoadEndMeasuredWithStore"
	}
	return buildPeakOverLoadEndMeasuredWithoutStore, "buildPeakOverLoadEndMeasuredWithoutStore"
}

// withFindingsStore reports whether the host's findings cache reads a store, which decides the mode the width's
// factors are taken in: it has none under `--no-cache`, under `--timing`, whose run has no session, or when the store
// could not be opened.
func (engine *pipeline) withFindingsStore() bool {
	return engine.session != nil && engine.session.HasStore()
}

// buildPeakFactor is the factor the run's width takes, in the run's mode (withFindingsStore).
func (engine *pipeline) buildPeakFactor() float64 {
	return buildPeakOverLoadEnd(engine.withFindingsStore())
}

// buildsWidth is how many of extras extra builds run side by side: as many as the memory cap holds beside the host's
// live heap, each build taken at its lint's peak, holding times the factor of the run's mode, with a findings store
// when withStore is true (buildPeakOverLoadEnd), floor((cap - hostLive) / (holding * factor)), with no more than the
// builds and at least one (#rmzehfg). Cores are no measure of it: the run's one pool bounds every build's work to them
// (#ckm0hsp). It returns too whether the slack added the last build.
//
// The slack is one build past that width, and never more, when it is fewer than the builds and one more fits by the
// largest ratio measured in the mode (buildPeakOverLoadEndMeasured) with buildsWidthSlackMarginBytes to spare:
// hostLive + (width + 1) * holding * measured + margin <= cap (#ewkp7w0). The factor's rounding up to the next half
// is a margin taken in units of a whole build's holding, about 0.36 GB at deeafbf4, whose `--no-cache` width sat at
// 3.00 and flipped to 2 about one run in four; the slack spends that margin only where the worst peak measured still
// fits with 512 MiB free, so the worst peak it permits is under the cap by construction, and passes it only for a tree
// whose ratio passes the largest measured, which a timed run names (recordBuildsPeakProblems).
//
// hostLiveBytes is the host's live heap as the builds start, which stays live until the last build ends (otherBuilds),
// and holdingBytes what each build is budgeted to hold beyond it as its load ends (budgetedHolding): the host's live
// heap when every share at the gate is low, and otherwise what the heaviest build holds, the bound taken for each
// build, since an extra build loads the same workspace under another environment, restoring from the host what holds
// there, so none holds more than the heaviest does. The cap is the limit the engine holds its heap under,
// engineMemoryCap, read from memory, which Main decided (capMemory).
//
// It is one at a time when:
//   - the run is single-threaded (`--single-threaded`), which asks for one thing at a time;
//   - memory is nil, a test that decided nothing, or hostLiveBytes or holdingBytes is 0, nothing measured, since then
//     neither the cap nor the heap is known;
//   - the run is under a limit other than the cap: half the memory available (source Available), whose ceiling is
//     what keeps runs side by side safe and a width sized against the cap would pass; one the environment set
//     (source Environment), whose owner decided the collector; or none it could read (source Unreadable), where the
//     collector runs at Go's default percent and no limit bounds the builds.
func buildsWidth(
	extras int,
	singleThreaded bool,
	memory *engineMemory,
	hostLiveBytes uint64,
	holdingBytes uint64,
	withStore bool,
) (int, bool) {
	if singleThreaded || memory == nil || memory.record.Source != contract.MemorySourceCap || hostLiveBytes == 0 ||
		holdingBytes == 0 {
		return 1, false
	}
	capBytes := uint64(max(memory.record.LimitBytes, 0))
	if capBytes <= hostLiveBytes {
		return 1, false
	}
	room := capBytes - hostLiveBytes
	// A peak past what a uint64 holds is past any cap, which the division reads as no room.
	peak := math.Ceil(float64(holdingBytes) * buildPeakOverLoadEnd(withStore))
	if peak >= math.MaxUint64 {
		return 1, false
	}
	// Bounded by extras before it is an int, so a tiny holding never overflows it.
	byMemory := int(min(room/uint64(peak), uint64(max(extras, 0))))
	width := max(min(extras, byMemory), 1)
	// A width already the builds' count has no build to add, and one past it would pass them; room under the margin
	// holds no build with it to spare.
	if width >= extras || room < buildsWidthSlackMarginBytes {
		return width, false
	}
	measured, _ := buildPeakOverLoadEndMeasured(withStore)
	// The room is exact in a float64 under 8 PiB, so the margin is spent to the byte.
	peaks := math.Ceil(float64(width+1) * float64(holdingBytes) * measured)
	if peaks > float64(room-buildsWidthSlackMarginBytes) {
		return width, false
	}
	return width + 1, true
}

// forcedLiveHeapBytes is the live heap measured by a collection run for it, so the width is decided by what the run
// holds at a fixed point, the host's as the extra builds start and the first build's as its load ends, and two runs of
// one workspace are given the same heap. The runtime's last collection, read without one, says the heap at whatever
// moment the collector last ran, which across three runs of one workspace gave the widths 1, 1 and 4.
//
// It returns once the collection's mark has terminated, when the live heap is set, rather than once runtime.GC has
// returned, which also sweeps every span the collection freed: at cohere's root the mark took 53 ms of runtime.GC's
// 219 ms (#geaqd90). The sweep goes on behind the run, on the goroutine runtime.GC runs on. Nothing else of the run
// allocates before the mark terminates, since at each fixed point nothing else runs.
func forcedLiveHeapBytes() uint64 {
	forced := []metrics.Sample{{Name: forcedCyclesMetric}}
	metrics.Read(forced)
	before, counted := forcedCycles(forced)
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		runtime.GC()
	}()
	for {
		select {
		case <-collected:
			return liveHeapBytes()
		default:
		}
		// A runtime that counts no forced cycles leaves the wait to runtime.GC's return.
		if counted {
			metrics.Read(forced)
			if after, _ := forcedCycles(forced); after > before {
				return liveHeapBytes()
			}
		}
		time.Sleep(markPollInterval)
	}
}

// lintPeakSampleInterval is how often heldWhileLinting takes the live heap under `--timing`: a platform build's lint at
// cohere's root runs one to six seconds, so its peak is sampled four to twenty-four times.
const lintPeakSampleInterval = 250 * time.Millisecond

// heldWhileLinting samples the live heap every interval by a forced collection (forcedLiveHeapBytes) until the stop it
// returns is called, which returns the largest live heap it saw, 0 when it saw none. Only `--timing` pays for it: each
// sample marks the whole heap beside the rules running, so the timed build's own times carry the collections.
func heldWhileLinting(interval time.Duration) func() uint64 {
	stop := make(chan struct{})
	largest := make(chan uint64, 1)
	go func() {
		var peak uint64
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				largest <- peak
				return
			case <-ticker.C:
				peak = max(peak, forcedLiveHeapBytes())
			}
		}
	}()
	return func() uint64 {
		close(stop)
		return <-largest
	}
}

// lintPeakOverLoadEnd is an extra build's ratio under `--timing`, what it held at its lint's peak over what it held as
// its load ended, to the thousandth, the ratio buildPeakOverLoadEnd's factors stand for, and 0 when either was not
// measured, in any run but a timed one and for the host's build.
func lintPeakOverLoadEnd(heldAtLoadEnd uint64, heldAtLintPeak uint64) float64 {
	if heldAtLoadEnd == 0 || heldAtLintPeak == 0 {
		return 0
	}
	return math.Round(float64(heldAtLintPeak)/float64(heldAtLoadEnd)*1000) / 1000
}

// recordBuildsPeakProblems names, in record's BuildsPeakProblems and on stderr, each build whose ratio
// (lintPeakOverLoadEnd) passes the largest measured in the run's mode (buildPeakOverLoadEndMeasured), which the
// width's slack spends its margin to: a tree that breaks the measured maximum is caught the first time a timed run
// measures it, rather than running its builds past the cap unseen (#ewkp7w0). A timed run has no session, so its builds
// are checked against the maximum measured without a store, the mode they ran in.
//
// Mutates: record gains BuildsPeakProblems
func (engine *pipeline) recordBuildsPeakProblems(record *contract.LintRecord) {
	measured, name := buildPeakOverLoadEndMeasured(engine.withFindingsStore())
	for _, build := range record.Builds {
		if build.LintPeakOverLoadEnd <= measured {
			continue
		}
		problem := fmt.Sprintf("the build %s held %.3f times at its lint's peak what it held as its load ended, past "+
			"%s, %.2f, the largest ratio measured, which the builds' width spends its slack to", build.Target,
			build.LintPeakOverLoadEnd, name, measured)
		record.BuildsPeakProblems = append(record.BuildsPeakProblems, problem)
		engine.note("%s", problem)
	}
}

// forcedCycles is the count a read of forcedCyclesMetric holds, and whether the runtime reports it.
func forcedCycles(forced []metrics.Sample) (uint64, bool) {
	if forced[0].Value.Kind() != metrics.KindUint64 {
		return 0, false
	}
	return forced[0].Value.Uint64(), true
}

// liveHeapBytes is the live heap at the runtime's last collection, 0 when it reports none.
func liveHeapBytes() uint64 {
	samples := []metrics.Sample{{Name: liveHeapMetric}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return samples[0].Value.Uint64()
}
