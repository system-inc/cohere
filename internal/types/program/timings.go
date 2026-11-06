package program

import (
	"runtime"
	"slices"
	"sort"
	"time"
)

// Timings collects per-rule cost across a run.
//
// The cost is the walk thread's own CPU clock, read around every call into a rule. Each walk worker is
// locked to its OS thread for the walk, so the clock it reads before a call and after it is the same
// thread's, and a thread's CPU clock does not move while the thread waits: a rule that sleeps, blocks,
// or is descheduled costs nothing here, which is the property the wall clock lacked.
//
// A CPU profile was the other candidate, and it was built and measured first. On macOS it does not
// work: the profiling signal is process-wide and lands on whichever thread the kernel picks, so a
// planted rule spinning 330ms of CPU across 80 files was billed 70 to 120ms, most of its samples
// landing on threads that were not running it (golang/go#14434 and #57722; Linux has per-thread timers
// since Go 1.18, macOS still does not). A table that is right only on Linux is wrong where it is read.
//
// The clock's price is about 100ns a read on darwin/arm64, so a call costs two reads where the wall
// clock cost two 28ns ones. That cost is measured on each worker before its walk (see calibrateFrame)
// and subtracted from every call, and what was subtracted is reported as the instrument's own CPU
// rather than left inside some rule's number.
type Timings struct {
	byRule map[string]*RuleTiming

	// sharedCpu is what each family of cached derivations cost, keyed by family (a cache key up to
	// its first colon, so `hir.Function:175:1001` counts toward `hir.Function`).
	//
	// Kept apart from any rule's total because it belongs to no rule. Whichever rule asks for a
	// derivation first pays for it, and files are walked in parallel, so that identity varies. Three
	// comment rules sharing one scan measured 171ms, 132ms, and 1.0ms for identical work; the cheap
	// one had simply asked last. Attributing shared cost to a rule is the instrument lying, and
	// reporting it separately is the only reading that survives a change in order.
	sharedCpu map[string]time.Duration

	// Account is the walk's CPU as a whole: what the workers spent, what of it no rule did, and what the
	// rest of the process did meanwhile.
	Account CpuAccount

	// meter is the worker measuring into this collector, set on a worker's local one while it walks.
	meter *workerMeter
}

// workerMeter is the meter measuring into this collector, nil when nothing is.
func (timings *Timings) workerMeter() *workerMeter {
	if timings == nil {
		return nil
	}
	return timings.meter
}

// CpuAccount is where a timed walk's CPU went, beyond the rules' own rows.
type CpuAccount struct {
	// Unavailable is why no CPU was measured, empty when it was: a platform with no thread CPU clock.
	Unavailable string

	// WorkersCpu is every walk worker thread's CPU from the start of its walk to the end.
	WorkersCpu time.Duration

	// WalkCpu is the part of WorkersCPU inside no rule, no shared derivation, and no measurement:
	// traversal, dispatch, configuration lookups and checker acquisition. It is the workers' total less
	// the measured parts, all read from the same threads' clocks, so it holds no waiting.
	WalkCpu time.Duration

	// InstrumentCpu is what the measurements themselves cost: the calibrated cost of one measured call,
	// times the calls. Subtracted from each call and reported here instead.
	InstrumentCpu time.Duration

	// Calls is how many calls into a rule or a shared derivation were measured.
	Calls int

	// OtherCpu is process CPU while the walk ran that no walk worker spent: the garbage collector,
	// the runtime, and anything running beside the walk.
	OtherCpu time.Duration
}

// NewTimings returns a collector ready for a run.
func NewTimings(ruleNames []string) *Timings {
	byRule := make(map[string]*RuleTiming, len(ruleNames))
	for _, name := range ruleNames {
		byRule[name] = &RuleTiming{Name: name}
	}
	return &Timings{byRule: byRule, sharedCpu: map[string]time.Duration{}}
}

// forRule returns the accumulator for a rule, creating it if the run added one late.
func (timings *Timings) forRule(name string) *RuleTiming {
	if timings == nil {
		return nil
	}
	timing, known := timings.byRule[name]
	if !known {
		timing = &RuleTiming{Name: name}
		timings.byRule[name] = timing
	}
	return timing
}

// merge folds one worker's local timings into the shared set.
//
// Workers measure locally and merge once under the mutex, the same way diagnostics and coverage
// already do. Measuring through a shared lock would put lock contention into the walk being measured.
func (timings *Timings) merge(other *Timings) {
	if timings == nil || other == nil {
		return
	}
	for family, cost := range other.sharedCpu {
		timings.sharedCpu[family] += cost
	}
	timings.Account.WorkersCpu += other.Account.WorkersCpu
	timings.Account.WalkCpu += other.Account.WalkCpu
	timings.Account.InstrumentCpu += other.Account.InstrumentCpu
	timings.Account.Calls += other.Account.Calls
	if timings.Account.Unavailable == "" {
		timings.Account.Unavailable = other.Account.Unavailable
	}
	for name, source := range other.byRule {
		target := timings.forRule(name)
		target.SetupCpu += source.SetupCpu
		target.ListenerCpu += source.ListenerCpu
		target.FilesListened += source.FilesListened
		target.FilesDeclined += source.FilesDeclined
		target.NodesOffered += source.NodesOffered
		target.Findings += source.Findings
	}
}

// Sorted returns every rule's timing, most expensive first.
//
// Sorted by cost so an outlier is obvious without arithmetic, with the name as a tiebreak so two
// runs over the same tree produce the same order and a diff between them means something.
func (timings *Timings) Sorted() []RuleTiming {
	if timings == nil {
		return nil
	}
	sorted := make([]RuleTiming, 0, len(timings.byRule))
	for _, timing := range timings.byRule {
		sorted = append(sorted, *timing)
	}
	sort.SliceStable(sorted, func(first, second int) bool {
		if sorted[first].TotalCpu() != sorted[second].TotalCpu() {
			return sorted[first].TotalCpu() > sorted[second].TotalCpu()
		}
		return sorted[first].Name < sorted[second].Name
	})
	return sorted
}

// SharedCpu is what each family of cached derivations cost across the run, keyed by family.
func (timings *Timings) SharedCpu() map[string]time.Duration {
	if timings == nil {
		return nil
	}
	return timings.sharedCpu
}

// TotalCpu is the summed cost of every rule.
func (timings *Timings) TotalCpu() time.Duration {
	if timings == nil {
		return 0
	}
	var total time.Duration
	for _, timing := range timings.byRule {
		total += timing.TotalCpu()
	}
	return total
}

// closeAccount bills the rest of the process's CPU during the walk, once every worker has merged.
// processStart is the process's CPU when the walk began.
func (timings *Timings) closeAccount(processStart time.Duration, processKnown bool) {
	if timings == nil || timings.Account.Unavailable != "" {
		return
	}
	if !processKnown {
		timings.Account.Unavailable = "the process's CPU could not be read"
		return
	}
	processEnd, known := processCpu()
	if !known {
		timings.Account.Unavailable = "the process's CPU could not be read"
		return
	}
	timings.Account.OtherCpu = max(processEnd-processStart-timings.Account.WorkersCpu, 0)
}

// frame is one open measurement.
type frame struct {
	start        time.Duration
	nestedBefore time.Duration
}

// startWorkerMeter locks the calling worker to its thread and starts measuring it. Nil when nothing is
// being timed, and nil with the reason recorded when this platform has no thread CPU clock.
//
// The lock is what makes two reads of the thread's clock a measurement of one goroutine: unlocked, a
// goroutine preempted mid-call can resume on another thread, and the delta would subtract one thread's
// clock from another's.
//
// Mutates: timings holds the meter started here, or why there is none, so the walk bills to it
func startWorkerMeter(timings *Timings) *workerMeter {
	if timings == nil {
		return nil
	}
	if _, known := threadCpu(); !known {
		timings.Account.Unavailable = "this platform has no thread CPU clock to read"
		return nil
	}
	runtime.LockOSThread()
	meter := &workerMeter{timings: timings, frameCost: calibrateFrame()}
	timings.meter = meter
	meter.start, _ = threadCpu()
	return meter
}

// calibrateFrame measures what one measured call costs when the call does nothing: two clock reads and
// the bookkeeping around them. Taken on the worker's own thread before its walk, as the mean of the
// middle eight tenths of many empty frames, so an interrupt landing in one does not set the figure. The
// clock steps in about 41ns on darwin/arm64, so the mean, not the median, is what averages that out.
func calibrateFrame() time.Duration {
	probe := &workerMeter{}
	costs := make([]time.Duration, 4096)
	for index := range costs {
		costs[index] = probe.close(probe.open())
	}
	slices.Sort(costs)
	middle := costs[len(costs)/10 : len(costs)*9/10]
	var sum time.Duration
	for _, cost := range middle {
		sum += cost
	}
	return sum / time.Duration(len(middle))
}

// SetForTest populates one rule's measured cost, for tests that need a known table without running
// a program. Not used outside tests.
func (timings *Timings) SetForTest(name string, cost time.Duration, nodesOffered int) {
	timing := timings.forRule(name)
	if timing == nil {
		return
	}
	timing.ListenerCpu = cost
	timing.NodesOffered = nodesOffered
	timing.FilesListened = 1
}

// SetSharedForTest populates one family of shared derivations' cost, for tests. Not used outside tests.
func (timings *Timings) SetSharedForTest(family string, cost time.Duration) {
	timings.sharedCpu[family] += cost
}
