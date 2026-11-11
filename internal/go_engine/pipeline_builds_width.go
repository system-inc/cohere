package go_engine

import (
	"runtime"
	"runtime/metrics"

	"github.com/system-inc/cohere/internal/go_engine/contract"
)

// liveHeapMetric is the runtime's live heap at its last collection, read without forcing one.
const liveHeapMetric = "/gc/heap/live:bytes"

// extraBuildsWidth is how many of the run's extras extra builds run side by side, decided once as they start
// (#b2t9xmf): a test's forced width when it set one, and otherwise buildsWidth over this process's cores, memory
// decision and live heap now. The width never reaches a key or the verdict, only the lint record's buildsAtOnce: the
// builds' results and notes are taken in build order whatever it is (extraBuildRuns).
func (engine *pipeline) extraBuildsWidth(extras int) int {
	if engine.buildsWidthForced > 0 {
		return min(engine.buildsWidthForced, max(extras, 1))
	}
	// The release's standard-library child collects every entry its sessions save into one map, which each session
	// writes under a lock of its own, so two builds saving at once would write it together. That child checks no
	// extra build today; one at a time keeps it safe if it ever does.
	if engine.request.standardLibrary.collected != nil {
		return 1
	}
	return buildsWidth(extras, runtime.GOMAXPROCS(0), engine.request.switches["single-threaded"], engine.memory,
		liveHeapBytes())
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

// liveHeapBytes is the live heap at the runtime's last collection, 0 when it reports none.
func liveHeapBytes() uint64 {
	samples := []metrics.Sample{{Name: liveHeapMetric}}
	metrics.Read(samples)
	if samples[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return samples[0].Value.Uint64()
}
