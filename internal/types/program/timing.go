package program

import (
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// RuleTiming is what one rule cost over a run, in CPU.
//
// CPU, not wall time, because wall time lied (#8qyzmxw). Each listener call used to be charged its
// wall time, and a goroutine descheduled mid-call was charged for the time it spent off the CPU. Under
// load that is most of the number: the better-tailwindcss family read about 1.2s on the old table,
// while eight interleaved cold pairs with the family off and on differed by a median of 0.049s, and a
// CPU profile put it under 1%. A rule called most often was interrupted most often, so the table
// ranked call counts and called them cost.
//
// Setup and listener time are kept apart because they are different defects. A rule burning time in
// Run and then declining the file is doing expensive work to decide it has nothing to do, which is
// the shape that made consistency-no-abbreviated-identifier 2,580ms of a 3,700ms run: it resolved a
// scope lookup on every identifier before testing whether the name even looked abbreviated. A cheap
// structural gate took it to 152ms, 17x, findings byte-identical. A rule burning time in its
// listeners is doing expensive work per node, which is a different fix.
type RuleTiming struct {
	Name string

	// SetupCpu is CPU inside the rule's Run function, deciding what to listen to.
	SetupCpu time.Duration

	// ListenerCpu is CPU inside the rule's listeners, walking nodes.
	ListenerCpu time.Duration

	// FilesListened is how many files the rule accepted, and FilesDeclined how many it refused.
	//
	// Declining is the cheapest and most valuable thing a rule can do, so a rule that declines
	// nothing is worth seeing: registering for a very common node kind and returning early is
	// legitimate, and it is also how a cheap-looking rule becomes expensive at scale.
	FilesListened int
	FilesDeclined int

	// NodesOffered is how many nodes the walk called this rule's listeners for.
	NodesOffered int

	// Findings is how many diagnostics the rule reported, before suppression.
	Findings int
}

// TotalCpu is what the rule cost altogether.
func (timing RuleTiming) TotalCpu() time.Duration {
	return timing.SetupCpu + timing.ListenerCpu
}

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

// workerMeter measures one walk worker: its thread's CPU across the walk, and each call it makes into a
// rule or a shared derivation.
//
// A call is a frame. Frames nest, since a listener can fill a cached derivation and one fill can ask for
// another, and each frame's own CPU is its clock delta less what the frames inside it took, which are
// billed where they belong. A nil workerMeter is a walk without --timing: every method passes straight
// through, so an ordinary run pays nothing.
type workerMeter struct {
	// timings is the worker's local collector, merged into the walk's when the worker finishes.
	timings *Timings

	// start is the thread's CPU when its walk began.
	start time.Duration

	// frameCost is what one measured call costs with nothing inside it, subtracted from every call.
	frameCost time.Duration

	// nested is the running total of frame deltas, from which each frame reads how much the frames
	// inside it took.
	nested time.Duration

	// depth is how many frames are open, and outermost the summed deltas of the frames opened at depth
	// zero, which is the part of the thread's CPU spent in measured calls.
	depth     int
	outermost time.Duration

	// calls is how many frames closed.
	calls int
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

// finish stops measuring the worker and unlocks its thread.
func (meter *workerMeter) finish() {
	if meter == nil {
		return
	}
	end, _ := threadCpu()
	runtime.UnlockOSThread()

	total := end - meter.start
	account := &meter.timings.Account
	account.WorkersCpu += total
	account.WalkCpu += max(total-meter.outermost, 0)
	account.InstrumentCpu += time.Duration(meter.calls) * meter.frameCost
	account.Calls += meter.calls
}

// open begins a frame.
func (meter *workerMeter) open() frame {
	meter.depth++
	now, _ := threadCpu()
	return frame{start: now, nestedBefore: meter.nested}
}

// close ends a frame and returns its own CPU: its delta, less the frames nested inside it and less what
// measuring it cost. Never negative: a frame cheaper than the measurement's average cost is billed zero.
func (meter *workerMeter) close(opened frame) time.Duration {
	now, _ := threadCpu()
	delta := now - opened.start
	inside := meter.nested - opened.nestedBefore

	// The enclosing frame, if there is one, sees this whole delta as nested work.
	meter.nested = opened.nestedBefore + delta
	meter.depth--
	if meter.depth == 0 {
		meter.outermost += delta
	}
	meter.calls++
	return max(delta-inside-meter.frameCost, 0)
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

// watchFills bills each of a file's cache fills to its derivation rather than to the rule that asked.
func (meter *workerMeter) watchFills(cache *rule.FileCache) {
	if meter == nil {
		return
	}
	cache.SetAroundFill(meter.aroundFill)
}

// setup runs a rule's Run as a frame billed to the rule's setup. The close is deferred because Run can
// panic and be recovered by the rule's containment, and the frames must still balance.
//
// Mutates: timing gains the CPU the rule's Run took, as its setup
func (meter *workerMeter) setup(timing *RuleTiming, run func()) {
	if meter == nil {
		run()
		return
	}
	opened := meter.open()
	defer func() { timing.SetupCpu += meter.close(opened) }()
	run()
}

// call calls a rule's listener on a node, counting the node and billing the call to the rule. Without a
// meter it only calls the listener.
//
// Mutates: timing counts the node offered and gains the CPU the listener took
func (meter *workerMeter) call(timing *RuleTiming, listener func(node *ast.Node), node *ast.Node) {
	if meter == nil {
		listener(node)
		return
	}
	timing.NodesOffered++
	opened := meter.open()
	defer func() { timing.ListenerCpu += meter.close(opened) }()
	listener(node)
}

// aroundFill runs one cache fill as a frame billed to its derivation's family: the key up to its first
// colon, so `hir.Function:175:1001` counts toward `hir.Function`. The rule that asked is billed its
// call less the fill, because the fill is a frame nested in its call.
func (meter *workerMeter) aroundFill(key string, fill func()) {
	family := key
	if colon := strings.IndexByte(key, ':'); colon >= 0 {
		family = key[:colon]
	}
	opened := meter.open()
	defer func() { meter.timings.sharedCpu[family] += meter.close(opened) }()
	fill()
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
