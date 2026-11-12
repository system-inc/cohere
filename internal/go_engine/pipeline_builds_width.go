package go_engine

import (
	"math"
	"runtime"
	"runtime/metrics"
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
// and true then, decided once as they start (#b2t9xmf): one at a time unless the builds restore packages from host, the
// host's run (restoresFromHost), a test's forced width when it set one, one at a time for the release's
// standard-library child, and one at a time when no heap could make it more (buildsWidth over the smallest heap). It is
// false when the width is the memory's to decide, by what the first build holds (measuredBuildsWidth). The width never
// reaches a key or the verdict, only the lint record's buildsAtOnce and the heap it was decided by: the builds' results
// and notes are taken in one order whatever it is (extraBuildRuns).
//
// Builds that restore nothing each check every package, about what the host's did: two of those side by side under
// the cap drive the collector, and at cohere's root made the run 6 to 8 s slower and cost about 70 user-s more, while
// builds that restore most of their packages from the host ran 22% faster side by side.
func (engine *pipeline) fixedBuildsWidth(extras int, host *check.HostRun) (int, bool) {
	if !engine.buildsWidthUnguarded && !engine.restoresFromHost(host) {
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
	if buildsWidth(extras, engine.request.switches["single-threaded"], engine.memory, 1, 1,
		engine.buildPeakFactor()) == 1 {
		return 1, true
	}
	return 0, false
}

// measuredBuildsWidth is how many of extras extra builds run side by side by what the first build to start holds
// (#rmzehfg), the heaviest the run has (heaviestFirst): the live heap with that build's load ended and nothing else
// running, measured by a collection run for it (forcedLiveHeapBytes), less hostLive, the host's live heap measured the
// same way as the builds started. It returns the width and the build's holding, at least one byte. The first build
// waits for it between its load and its rules, holding none of the run's slots, and the rest start once it is decided
// (extraBuildRuns).
func (engine *pipeline) measuredBuildsWidth(extras int, hostLive uint64) (int, uint64) {
	holding := holdingAbove(forcedLiveHeapBytes(), hostLive)
	return buildsWidth(extras, engine.request.switches["single-threaded"], engine.memory, hostLive, holding,
		engine.buildPeakFactor()), holding
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

// heaviestFirst is the order the extra builds start in, as indexes of extras: every build on a platform other than
// host's first, then the builds on host's platform, each in build order, so the first to start, the one the width is
// measured by (measuredBuildsWidth), is among the heaviest. A build on another platform holds the most, since the
// standard library's types differ there and its packages restore least from the host's run: on Nov 11 the platform
// builds of cohere's root held 0.90 to 1.17 GB beyond the host's heap as their loads ended, and its tag builds, which
// restore nearly every package, 0.13 to 0.14 GB (#rmzehfg). The merge takes the runs in build order whatever order
// they started in, and stderr their notes in this one (extraBuildRuns).
func heaviestFirst(host project.Build, extras []project.Build) []int {
	var otherPlatforms []int
	var hostPlatform []int
	for index, extra := range extras {
		if extra.OperatingSystem == host.OperatingSystem && extra.Architecture == host.Architecture {
			hostPlatform = append(hostPlatform, index)
		} else {
			otherPlatforms = append(otherPlatforms, index)
		}
	}
	return append(otherPlatforms, hostPlatform...)
}

// restoresFromHost reports whether the extra builds will restore packages from host, the host's run: it kept entries
// for them, and nothing keeps the builds from reading them, a test turning the restore off or a run whose rename wrote,
// whose builds read no findings cache. A run under `--timing` keeps no entries.
func (engine *pipeline) restoresFromHost(host *check.HostRun) bool {
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
// one run under `--timing`, whose build records carry each build's holding at both points, and a constant changes only
// on such a record.
const (
	buildPeakOverLoadEndWithStore    = 3.5
	buildPeakOverLoadEndWithoutStore = 2.5
)

// buildPeakOverLoadEnd is the factor for a run with a findings store when withStore is true, and for one without.
func buildPeakOverLoadEnd(withStore bool) float64 {
	if withStore {
		return buildPeakOverLoadEndWithStore
	}
	return buildPeakOverLoadEndWithoutStore
}

// buildPeakFactor is the factor the run's width takes: the host's findings cache reads a store, or it has none, under
// `--no-cache` or when the store could not be opened.
func (engine *pipeline) buildPeakFactor() float64 {
	return buildPeakOverLoadEnd(engine.session != nil && engine.session.HasStore())
}

// buildsWidth is how many of extras extra builds run side by side: as many as the memory cap holds beside the host's
// live heap, each build taken at its lint's peak, holding times factor (buildPeakFactor), floor((cap - hostLive) /
// (holding * factor)), with no more than the builds and at least one (#rmzehfg). Cores are no measure of it: the run's
// one pool bounds every build's work to them (#ckm0hsp).
//
// hostLiveBytes is the host's live heap as the builds start, which stays live until the last build ends (otherBuilds),
// and holdingBytes what the heaviest build holds beyond it as its load ends, the bound taken for each build: an extra
// build loads the same workspace under another environment, restoring from the host what holds there, so none holds
// more than the heaviest does. The cap is the limit the engine holds its heap under, engineMemoryCap, read from memory,
// which Main decided (capMemory).
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
	factor float64,
) int {
	if singleThreaded || memory == nil || memory.record.Source != contract.MemorySourceCap || hostLiveBytes == 0 ||
		holdingBytes == 0 {
		return 1
	}
	capBytes := uint64(max(memory.record.LimitBytes, 0))
	if capBytes <= hostLiveBytes {
		return 1
	}
	// A peak past what a uint64 holds is past any cap, which the division reads as no room.
	peak := math.Ceil(float64(holdingBytes) * factor)
	if peak >= math.MaxUint64 {
		return 1
	}
	// Bounded by extras before it is an int, so a tiny holding never overflows it.
	byMemory := int(min((capBytes-hostLiveBytes)/uint64(peak), uint64(max(extras, 0))))
	return max(min(extras, byMemory), 1)
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
