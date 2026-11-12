package program

import (
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
	"github.com/system-inc/cohere/internal/cpu_clock"
	"github.com/system-inc/cohere/internal/lint/rule"
	"github.com/system-inc/cohere/internal/needs"
)

// TestSortedPutsTheExpensiveRuleFirst is what makes an outlier obvious without arithmetic.
func TestSortedPutsTheExpensiveRuleFirst(test *testing.T) {
	test.Parallel()
	timings := NewTimings([]string{"cheap", "expensive", "middling"})
	timings.forRule("cheap").ListenerCpu = time.Millisecond
	timings.forRule("expensive").ListenerCpu = 500 * time.Millisecond
	timings.forRule("middling").ListenerCpu = 50 * time.Millisecond

	sorted := timings.Sorted()
	if len(sorted) != 3 {
		test.Fatalf("expected 3 rules, got %d", len(sorted))
	}
	if sorted[0].Name != "expensive" {
		test.Fatalf("the most expensive rule is not first: %v", sorted[0].Name)
	}
	if sorted[2].Name != "cheap" {
		test.Fatalf("the cheapest rule is not last: %v", sorted[2].Name)
	}
}

// TestSetupAndListenerTimeStaySeparate pins a distinction that decides which fix a slow rule needs.
//
// Time in setup means a rule is doing expensive work to decide it has nothing to do, which is the
// shape that made one rule 2,580ms of a 3,700ms run under the previous gate. Time in listeners
// means expensive work per node. The fixes have nothing in common, so a single total would hide
// the more actionable of the two.
func TestSetupAndListenerTimeStaySeparate(test *testing.T) {
	test.Parallel()
	timings := NewTimings([]string{"decides-slowly"})
	timing := timings.forRule("decides-slowly")
	timing.SetupCpu = 300 * time.Millisecond
	timing.ListenerCpu = 2 * time.Millisecond

	sorted := timings.Sorted()
	if sorted[0].SetupCpu != 300*time.Millisecond {
		test.Fatalf("setup time was lost: %v", sorted[0].SetupCpu)
	}
	if sorted[0].ListenerCpu != 2*time.Millisecond {
		test.Fatalf("listener time was lost: %v", sorted[0].ListenerCpu)
	}
	if sorted[0].TotalCpu() != 302*time.Millisecond {
		test.Fatalf("total does not sum its parts: %v", sorted[0].TotalCpu())
	}
}

// TestMergeAccumulatesAcrossWorkers covers the path every real run takes.
//
// Workers count locally and merge once under the mutex, because counting through a shared lock would
// put contention into the walk being measured. A merge that dropped or double-counted would make every
// number quietly wrong in a way no single-worker test could see. CPU is not merged: it comes from the
// one profile of the whole walk.
func TestMergeAccumulatesAcrossWorkers(test *testing.T) {
	test.Parallel()
	shared := NewTimings([]string{"a-rule"})

	for worker := 0; worker < 4; worker++ {
		local := NewTimings(nil)
		timing := local.forRule("a-rule")
		timing.NodesOffered = 100
		timing.FilesListened = 5
		timing.FilesDeclined = 2
		timing.Findings = 3
		shared.merge(local)
	}

	merged := shared.Sorted()[0]
	if merged.NodesOffered != 400 || merged.FilesListened != 20 || merged.FilesDeclined != 8 || merged.Findings != 12 {
		test.Fatalf("counts did not accumulate: %+v", merged)
	}
}

// TestMergePicksUpARuleTheRunAddedLate keeps a worker's rule from being dropped because the shared
// collector was built without it.
func TestMergePicksUpARuleTheRunAddedLate(test *testing.T) {
	test.Parallel()
	shared := NewTimings(nil)
	local := NewTimings(nil)
	local.forRule("late-rule").NodesOffered = 1
	shared.merge(local)

	if len(shared.Sorted()) != 1 {
		test.Fatalf("a rule known only to a worker was dropped: %+v", shared.Sorted())
	}
}

// TestNilCollectorIsInert is the guard that keeps an ordinary run free.
//
// Every timing site is nil-checked so a run without --timing reads no clocks and allocates nothing.
// If that ever stops being true, the instrument starts costing what it was designed not to.
func TestNilCollectorIsInert(test *testing.T) {
	test.Parallel()
	var timings *Timings

	timings.merge(NewTimings([]string{"anything"}))

	if timings.Sorted() != nil {
		test.Fatal("a nil collector produced rows")
	}
	if timings.TotalCpu() != 0 {
		test.Fatal("a nil collector reported time")
	}
	if timings.forRule("anything") != nil {
		test.Fatal("a nil collector handed out an accumulator")
	}
}

// spinFor burns at least cpu of this thread's CPU, by the thread's own clock, so it is CPU however the
// scheduler treats it, and returns what it actually spent.
//
// At least, not exactly: the spin stops at the first read past cpu, and a read can land well past it. On
// a quiet machine the step is the clock's, about 100ns, but on a loaded virtual machine one jump between
// two reads can be hundreds of microseconds. So a test holds the meter to what the spin returns, never to
// the cpu it asked for. 0.2.0's dry run on a 3-core macos-26 runner read 3.656ms billed against a 3ms
// ask, and it kept no record of what the spin spent, so it could not say which of the two was off.
func spinFor(test *testing.T, cpu time.Duration) time.Duration {
	test.Helper()
	start, known := cpu_clock.Thread()
	if !known {
		test.Skip(
			"this platform has no thread CPU clock, so --timing reports no CPU here and the meter has nothing to test",
		)
	}
	for {
		if now, _ := cpu_clock.Thread(); now-start >= cpu {
			return now - start
		}
	}
}

// within reports whether got is want give or take a tenth, plus a little for the clock's step and the
// meter's own bookkeeping. want is what a spin returned, not what it asked for.
func within(got time.Duration, want time.Duration) bool {
	slack := want/10 + 200*time.Microsecond
	return got >= want-slack && got <= want+slack
}

// startTestMeter starts a meter on the test's goroutine and finishes it when the test ends.
func startTestMeter(test *testing.T) (*workerMeter, *Timings) {
	test.Helper()
	timings := NewTimings(nil)
	meter := startWorkerMeter(timings)
	if meter == nil {
		// Only a platform without a thread CPU clock starts no meter, and there --timing says so rather than
		// measuring; see cpu_clock_other.go.
		test.Skipf("no meter on this platform: %s", timings.Account.Unavailable)
	}
	test.Cleanup(meter.finish)
	return meter, timings
}

// TestAMeteredCallIsBilledItsCpu covers the wrapper the whole table rests on. Five calls each spinning 2ms
// of CPU are billed 10ms, and each call is counted as a node offered.
func TestAMeteredCallIsBilledItsCpu(test *testing.T) {
	test.Parallel()
	meter, _ := startTestMeter(test)
	timing := &RuleTiming{Name: "spins"}
	var spent time.Duration
	spins := func(node *ast.Node) { spent += spinFor(test, 2*time.Millisecond) }

	for range 5 {
		meter.call(timing, spins, nil)
	}

	if timing.NodesOffered != 5 {
		test.Fatalf("calls were not counted: %d", timing.NodesOffered)
	}
	if !within(timing.ListenerCpu, spent) {
		test.Fatalf("five calls spinning %v in all were billed %v", spent, timing.ListenerCpu)
	}
}

// TestAWaitingCallCostsNothing is the property the wall clock lacked (#8qyzmxw). Five calls each sleeping
// 5ms spend 25ms of wall time and almost no CPU, and they are billed the CPU.
func TestAWaitingCallCostsNothing(test *testing.T) {
	test.Parallel()
	meter, _ := startTestMeter(test)
	timing := &RuleTiming{Name: "sleeps"}
	sleeps := func(node *ast.Node) { time.Sleep(5 * time.Millisecond) }

	wallStart := time.Now()
	for range 5 {
		meter.call(timing, sleeps, nil)
	}
	wall := time.Since(wallStart)

	if wall < 25*time.Millisecond {
		test.Fatalf("the calls slept %v, under the 25ms planted, so they cannot show a wall clock's error", wall)
	}
	// Relative to the sleep, so it holds at any load: a busy machine stretches the sleep and the few
	// instructions around it alike. A wall clock bills all of it, about 100%.
	if timing.ListenerCpu > wall/20 {
		test.Fatalf("five calls that only slept were billed %v of CPU, having slept %v", timing.ListenerCpu, wall)
	}
}

// TestSetupIsBilledApartFromListeners keeps the distinction that decides which fix a slow rule needs.
func TestSetupIsBilledApartFromListeners(test *testing.T) {
	test.Parallel()
	meter, _ := startTestMeter(test)
	timing := &RuleTiming{Name: "decides"}
	var spent time.Duration
	meter.setup(timing, func() { spent = spinFor(test, 3*time.Millisecond) })

	if !within(timing.SetupCpu, spent) || timing.ListenerCpu != 0 {
		test.Fatalf("a setup spinning %v was billed setup %v and listen %v", spent, timing.SetupCpu, timing.ListenerCpu)
	}
}

// TestSharedFillIsBilledToTheDerivationNotTheRuleThatAsked is the fix for an instrument that lied.
//
// A cached derivation is computed by whichever rule asks first, and files are walked in parallel,
// so that rule is arbitrary. Measured on three comment rules sharing one scan before this existed:
// 171ms, 132ms, and 1.0ms for identical work. The cheap one had simply asked last, and a reader
// would have concluded the 171ms rule was expensive and optimized the wrong thing.
//
// So a fill is a frame of its own, billed to its derivation's family, and the frame around it is billed
// less the fill: the rule that asked, or an outer fill when one derivation fills another. The family is
// the key up to its first colon, because the `HIR` cache keys per function node and once reported 11,150
// rows of 0.00ms.
func TestSharedFillIsBilledToTheDerivationNotTheRuleThatAsked(test *testing.T) {
	test.Parallel()
	meter, timings := startTestMeter(test)
	cache := rule.NewFileCache()
	meter.watchFills(cache)
	timing := &RuleTiming{Name: "asked-first"}
	var askerSpent time.Duration
	var outerSpent time.Duration
	var innerSpent time.Duration

	meter.call(timing, func(node *ast.Node) {
		askerSpent = spinFor(test, 2*time.Millisecond)
		rule.Cached(cache, "comments.All", func() int {
			outerSpent = spinFor(test, 3*time.Millisecond)
			rule.Cached(cache, "hir.Function:175:1001", func() int {
				innerSpent = spinFor(test, 4*time.Millisecond)
				return 1
			})
			return 1
		})
	}, nil)

	if !within(timing.ListenerCpu, askerSpent) {
		test.Fatalf("the rule that asked was billed %v for its own %v", timing.ListenerCpu, askerSpent)
	}
	if got := timings.SharedCpu()["comments.All"]; !within(got, outerSpent) {
		test.Fatalf("the outer derivation was billed %v for its own %v", got, outerSpent)
	}
	if got := timings.SharedCpu()["hir.Function"]; !within(got, innerSpent) {
		test.Fatalf("the per-node derivation was billed %v to its family for its %v", got, innerSpent)
	}
	if cache.Fills()["comments.All"] != 1 || cache.Fills()["hir.Function:175:1001"] != 1 {
		test.Fatalf("each derivation should have been computed once: %v", cache.Fills())
	}
}

// TestARuleThatPanicsStillBalancesItsFrames covers the crash path. A rule's panic is recovered by its
// containment and the walk goes on, so a frame that closed only on a normal return would leave every
// later call nested inside the crashed one, billed against it.
func TestARuleThatPanicsStillBalancesItsFrames(test *testing.T) {
	test.Parallel()
	meter, _ := startTestMeter(test)
	timing := &RuleTiming{Name: "crashes"}
	recovering := func(run func()) {
		defer func() { _ = recover() }()
		run()
	}

	recovering(func() { meter.call(timing, func(node *ast.Node) { panic("planted") }, nil) })
	recovering(func() { meter.setup(timing, func() { panic("planted") }) })

	if meter.depth != 0 {
		test.Fatalf("frames were left open after a recovered panic: depth %d", meter.depth)
	}
}

// TestTheWorkersCpuIsAccountedFor holds that the rows and the account add up to what the worker's thread
// spent, so nothing the table prints is a residual it cannot name.
func TestTheWorkersCpuIsAccountedFor(test *testing.T) {
	test.Parallel()
	needs.NoRace(test, "the walk's own share is held to what a 2ms spin spent, and the race detector's "+
		"instrumentation of the meter bills it several times that (8.8ms on Home, #bnabymg)")
	timings := NewTimings(nil)
	meter := startWorkerMeter(timings)
	if meter == nil {
		test.Skipf("no meter on this platform: %s", timings.Account.Unavailable)
	}
	timing := timings.forRule("spins")
	outsideSpent := spinFor(test, 2*time.Millisecond)
	meter.call(timing, func(node *ast.Node) { spinFor(test, 3*time.Millisecond) }, nil)
	meter.finish()

	account := timings.Account
	if account.Calls != 1 {
		test.Fatalf("one call was counted as %d", account.Calls)
	}
	if !within(account.WalkCpu, outsideSpent) {
		test.Fatalf("%v spent outside any call was billed to the walk as %v", outsideSpent, account.WalkCpu)
	}
	parts := timing.ListenerCpu + account.InstrumentCpu + account.WalkCpu
	if difference := account.WorkersCpu - parts; difference < -time.Microsecond || difference > time.Microsecond {
		test.Fatalf("the worker spent %v and its parts sum to %v", account.WorkersCpu, parts)
	}
}

// TestTheMeterIsPassThroughWhenNotTiming proves the ordinary path costs nothing: no meter, the listener
// itself rather than a wrapper, and a cache whose fills run as they always did.
func TestTheMeterIsPassThroughWhenNotTiming(test *testing.T) {
	test.Parallel()
	meter := startWorkerMeter(nil)
	if meter != nil {
		test.Fatal("a walk without --timing started a meter")
	}
	cache := rule.NewFileCache()
	meter.watchFills(cache)

	called := 0
	meter.call(nil, func(node *ast.Node) { called++ }, nil)
	meter.setup(nil, func() { called++ })
	meter.finish()
	if called != 2 {
		test.Fatalf("the rule was not called through: %d of 2", called)
	}
	if got := rule.Cached(cache, "a.derivation", func() int { return 7 }); got != 7 {
		test.Fatalf("an unmetered cache did not fill: %d", got)
	}
}
