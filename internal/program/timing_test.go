package program

import (
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/shim/ast"
)

// TestSortedPutsTheExpensiveRuleFirst is what makes an outlier obvious without arithmetic.
func TestSortedPutsTheExpensiveRuleFirst(t *testing.T) {
	timings := NewTimings([]string{"cheap", "expensive", "middling"})
	timings.forRule("cheap").ListenerDuration = time.Millisecond
	timings.forRule("expensive").ListenerDuration = 500 * time.Millisecond
	timings.forRule("middling").ListenerDuration = 50 * time.Millisecond

	sorted := timings.Sorted()
	if len(sorted) != 3 {
		t.Fatalf("expected 3 rules, got %d", len(sorted))
	}
	if sorted[0].Name != "expensive" {
		t.Fatalf("the most expensive rule is not first: %v", sorted[0].Name)
	}
	if sorted[2].Name != "cheap" {
		t.Fatalf("the cheapest rule is not last: %v", sorted[2].Name)
	}
}

// TestSetupAndListenerTimeStaySeparate pins a distinction that decides which fix a slow rule needs.
//
// Time in setup means a rule is doing expensive work to decide it has nothing to do, which is the
// shape that made one rule 2,580ms of a 3,700ms run under the previous gate. Time in listeners
// means expensive work per node. The fixes have nothing in common, so a single total would hide
// the more actionable of the two.
func TestSetupAndListenerTimeStaySeparate(t *testing.T) {
	timings := NewTimings([]string{"decides-slowly"})
	timing := timings.forRule("decides-slowly")
	timing.SetupDuration = 300 * time.Millisecond
	timing.ListenerDuration = 2 * time.Millisecond

	sorted := timings.Sorted()
	if sorted[0].SetupDuration != 300*time.Millisecond {
		t.Fatalf("setup time was lost: %v", sorted[0].SetupDuration)
	}
	if sorted[0].ListenerDuration != 2*time.Millisecond {
		t.Fatalf("listener time was lost: %v", sorted[0].ListenerDuration)
	}
	if sorted[0].TotalDuration() != 302*time.Millisecond {
		t.Fatalf("total does not sum its parts: %v", sorted[0].TotalDuration())
	}
}

// TestMergeAccumulatesAcrossWorkers covers the path every real run takes.
//
// Workers time locally and merge once under the mutex, because timing through a shared lock would
// measure contention rather than rule cost. A merge that dropped or double-counted would make every
// number quietly wrong in a way no single-worker test could see.
func TestMergeAccumulatesAcrossWorkers(t *testing.T) {
	shared := NewTimings([]string{"a-rule"})

	for worker := 0; worker < 4; worker++ {
		local := NewTimings(nil)
		timing := local.forRule("a-rule")
		timing.ListenerDuration = 10 * time.Millisecond
		timing.SetupDuration = time.Millisecond
		timing.NodesOffered = 100
		timing.FilesListened = 5
		timing.FilesDeclined = 2
		timing.Findings = 3
		shared.merge(local)
	}

	merged := shared.Sorted()[0]
	if merged.ListenerDuration != 40*time.Millisecond {
		t.Fatalf("listener time did not accumulate: %v", merged.ListenerDuration)
	}
	if merged.NodesOffered != 400 || merged.FilesListened != 20 || merged.FilesDeclined != 8 || merged.Findings != 12 {
		t.Fatalf("counts did not accumulate: %+v", merged)
	}
}

// TestMergePicksUpARuleTheRunAddedLate keeps a worker's rule from being dropped because the shared
// collector was built without it.
func TestMergePicksUpARuleTheRunAddedLate(t *testing.T) {
	shared := NewTimings(nil)
	local := NewTimings(nil)
	local.forRule("late-rule").ListenerDuration = time.Millisecond
	shared.merge(local)

	if len(shared.Sorted()) != 1 {
		t.Fatalf("a rule known only to a worker was dropped: %+v", shared.Sorted())
	}
}

// TestNilCollectorIsInert is the guard that keeps an ordinary run free.
//
// Every timing site is nil-checked so a run without --timing reads no clocks and allocates nothing.
// If that ever stops being true, the instrument starts costing what it was designed not to.
func TestNilCollectorIsInert(t *testing.T) {
	var timings *Timings

	timings.merge(NewTimings([]string{"anything"}))

	if timings.Sorted() != nil {
		t.Fatal("a nil collector produced rows")
	}
	if timings.TotalDuration() != 0 {
		t.Fatal("a nil collector reported time")
	}
	if timings.forRule("anything") != nil {
		t.Fatal("a nil collector handed out an accumulator")
	}
}

// TestMeasuringListenerCountsAndTimes covers the wrapper the whole table rests on.
func TestMeasuringListenerCountsAndTimes(t *testing.T) {
	timing := &RuleTiming{Name: "measured"}
	wrapped := measuringListener(timing, func(node *ast.Node) {
		time.Sleep(time.Millisecond)
	})

	wrapped(nil)
	wrapped(nil)

	if timing.NodesOffered != 2 {
		t.Fatalf("nodes were not counted: %d", timing.NodesOffered)
	}
	if timing.ListenerDuration < 2*time.Millisecond {
		t.Fatalf("listener time was not accumulated: %v", timing.ListenerDuration)
	}
}

// TestMeasuringListenerIsPassThroughWhenNotTiming proves the zero-cost path is actually zero cost:
// the same function comes back, not a wrapper around it.
func TestMeasuringListenerIsPassThroughWhenNotTiming(t *testing.T) {
	called := 0
	original := func(node *ast.Node) { called++ }

	measuringListener(nil, original)(nil)

	if called != 1 {
		t.Fatal("the listener was not called through")
	}
}

// TestSharedFillIsBilledToTheCacheNotAVictimRule is the fix for an instrument that lied.
//
// A cached derivation is computed by whichever rule asks first, and files are walked in parallel,
// so that rule is arbitrary. Measured on three comment rules sharing one scan before this existed:
// 171ms, 132ms, and 1.0ms for identical work. The cheap one had simply asked last, and a reader
// would have concluded the 171ms rule was expensive and optimized the wrong thing.
//
// The number that made it visible: two rules offered exactly the same 3,407 nodes differed by 83x.
// Equal nodes with wildly unequal time is the signature of cost that belongs to neither.
func TestSharedFillIsBilledToTheCacheNotAVictimRule(t *testing.T) {
	timings := NewTimings([]string{"asked-first", "asked-second"})

	// Both rules did 10ms of their own work; the first also paid 100ms to fill a shared cache.
	timings.forRule("asked-first").ListenerDuration = 110 * time.Millisecond
	timings.forRule("asked-second").ListenerDuration = 10 * time.Millisecond

	timings.RecordSharedFill("asked-first", "a.derivation", 100*time.Millisecond)

	first := timings.forRule("asked-first")
	if first.ListenerDuration != 10*time.Millisecond {
		t.Fatalf("the shared cost stayed on the rule that paid it: %v", first.ListenerDuration)
	}
	if timings.forRule("asked-second").ListenerDuration != 10*time.Millisecond {
		t.Fatal("the rule that did not pay was altered")
	}
	if timings.SharedFills()["a.derivation"] != 100*time.Millisecond {
		t.Fatalf("the shared cost was not recorded against the derivation: %v", timings.SharedFills())
	}
}

// TestSharedFillCannotDriveARuleNegative guards the subtraction.
//
// A fill recorded larger than the rule's measured time would otherwise produce a negative duration,
// which formats as a nonsense number rather than failing. Clamping is the honest floor: the rule
// did at least zero work.
func TestSharedFillCannotDriveARuleNegative(t *testing.T) {
	timings := NewTimings([]string{"a-rule"})
	timings.forRule("a-rule").ListenerDuration = time.Millisecond

	timings.RecordSharedFill("a-rule", "a.derivation", time.Second)

	if got := timings.forRule("a-rule").ListenerDuration; got < 0 {
		t.Fatalf("a rule's time went negative: %v", got)
	}
}

// TestSharedFillsMergeAcrossWorkers covers the path a real run takes, where each worker fills its
// own files' caches and the totals have to add up.
func TestSharedFillsMergeAcrossWorkers(t *testing.T) {
	shared := NewTimings([]string{"a-rule"})

	for worker := 0; worker < 4; worker++ {
		local := NewTimings(nil)
		local.forRule("a-rule").ListenerDuration = 50 * time.Millisecond
		local.RecordSharedFill("a-rule", "a.derivation", 25*time.Millisecond)
		shared.merge(local)
	}

	if got := shared.SharedFills()["a.derivation"]; got != 100*time.Millisecond {
		t.Fatalf("shared fills did not accumulate across workers: %v", got)
	}
}
