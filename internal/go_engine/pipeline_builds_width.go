package go_engine

import (
	"runtime"
	"runtime/metrics"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// liveHeapMetric is the runtime's live heap at its last collection, read without forcing one.
const liveHeapMetric = "/gc/heap/live:bytes"

// extraBuildsWidth is how many of the run's extras extra builds run side by side, decided once as they start
// (#b2t9xmf), and the host's live heap it was decided by, 0 when it needed none: one at a time unless the builds
// restore packages from host, the host's run (restoresFromHost), then a test's forced width when it set one, and
// otherwise buildsWidth over this process's cores, memory decision and the host's live heap (hostLiveHeapBytes). The
// width never reaches a key or the verdict, only the lint record's buildsAtOnce and buildsWidthLiveBytes: the builds'
// results and notes are taken in build order whatever it is (extraBuildRuns).
//
// Builds that restore nothing each check every package, about what the host's did: two of those side by side under
// the cap drive the collector, and at cohere's root made the run 6 to 8 s slower and cost about 70 user-s more, while
// builds that restore most of their packages from the host ran 22% faster side by side.
func (engine *pipeline) extraBuildsWidth(extras int, host *check.HostRun) (int, uint64) {
	return engine.extraBuildsWidthOver(extras, host, runtime.GOMAXPROCS(0))
}

// extraBuildsWidthOver is extraBuildsWidth over processors, which a test sets, since the pool its tests run in caps
// this process's.
func (engine *pipeline) extraBuildsWidthOver(extras int, host *check.HostRun, processors int) (int, uint64) {
	if !engine.buildsWidthUnguarded && !engine.restoresFromHost(host) {
		return 1, 0
	}
	if engine.buildsWidthForced > 0 {
		return min(engine.buildsWidthForced, max(extras, 1)), 0
	}
	// The release's standard-library child collects every entry its sessions save into one map, which each session
	// writes under a lock of its own, so two builds saving at once would write it together. That child checks no
	// extra build today; one at a time keeps it safe if it ever does.
	if engine.request.standardLibrary.collected != nil {
		return 1, 0
	}
	singleThreaded := engine.request.switches["single-threaded"]
	// A live heap of one byte is the smallest any heap can be, so the widest the cores, the builds and the limit allow:
	// when even that is one at a time, no heap can widen it, and the run pays no collection to measure one.
	if buildsWidth(extras, processors, singleThreaded, engine.memory, 1) == 1 {
		return 1, 0
	}
	live := hostLiveHeapBytes()
	return buildsWidth(extras, processors, singleThreaded, engine.memory, live), live
}

// restoresFromHost reports whether the extra builds will restore packages from host, the host's run: it kept entries
// for them, and nothing keeps the builds from reading them, a test turning the restore off or a run whose rename wrote,
// whose builds read no findings cache. A run under `--timing` keeps no entries.
func (engine *pipeline) restoresFromHost(host *check.HostRun) bool {
	return host.EntriesKept() > 0 && !engine.keepsHostRestoreOff && !engine.renamed
}

// buildsWidth is how many of extras extra builds run side by side: the fewest of the builds, a quarter of processors,
// since each build's own load and lint already keep about four cores busy, and how many more builds the memory cap
// holds beside the live heap, floor((cap - live) / live), with at least one.
//
// liveBytes is the host's live heap at its last collection, the bound taken for each build: an extra build loads the
// same workspace under another environment, so it holds about what the host's did. The cap is the limit the engine
// holds its heap under, engineMemoryCap, read from memory, which Main decided (capMemory).
//
// It is one at a time when:
//   - the run is single-threaded (`--single-threaded`), which asks for one thing at a time;
//   - memory is nil, a test that decided nothing, or liveBytes is 0, the runtime having reported no collection yet,
//     since then neither the cap nor the live heap is known;
//   - the run is under a limit other than the cap: half the memory available (source Available), whose ceiling is
//     what keeps runs side by side safe and a width sized against the cap would pass; one the environment set
//     (source Environment), whose owner decided the collector; or none it could read (source Unreadable), where the
//     collector runs at Go's default percent and no limit bounds the builds.
func buildsWidth(
	extras int,
	processors int,
	singleThreaded bool,
	memory *engineMemory,
	liveBytes uint64,
) int {
	if singleThreaded || memory == nil || memory.record.Source != contract.MemorySourceCap || liveBytes == 0 {
		return 1
	}
	capBytes := uint64(max(memory.record.LimitBytes, 0))
	if capBytes <= liveBytes {
		return 1
	}
	// Bounded by extras before it is an int, so a tiny live heap never overflows it.
	byMemory := int(min((capBytes-liveBytes)/liveBytes, uint64(max(extras, 0))))
	return max(min(extras, processors/4, byMemory), 1)
}

// hostLiveHeapBytes is the host's live heap as the extra builds start, measured by a collection run for it, so the
// width is decided by what the host's run holds at one fixed point, once its reads are whole (otherBuilds), and two
// runs of one workspace are given the same heap. The runtime's last collection, read without one, says the heap at
// whatever moment the collector last ran, which across three runs of one workspace gave the widths 1, 1 and 4.
func hostLiveHeapBytes() uint64 {
	runtime.GC()
	return liveHeapBytes()
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
