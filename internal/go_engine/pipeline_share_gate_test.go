package go_engine

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/cache"
	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// sharedPackages is how many packages every build of lowShareBuilds compiles alike, enough that the few each build
// compiles its own way are a small share of its graph.
const sharedPackages = 40

// lowShareBuilds is threeExtraBuilds with sharedPackages more packages every build compiles alike, each restored from
// the host's run, so every build's share at the share gate is under lowShareAtGate: the tagged build, windows and the
// third platform each compile a package their own way, which alone is a quarter of windows' small graph.
func lowShareBuilds(test *testing.T) string {
	test.Helper()
	root := threeExtraBuilds(test)
	writeSharedPackages(test, root)
	return root
}

// writeSharedPackages writes sharedPackages packages under root that every build compiles alike.
func writeSharedPackages(test *testing.T, root string) {
	test.Helper()
	files := map[string]string{}
	for index := range sharedPackages {
		files[fmt.Sprintf("internal/shared%d/shared.go", index)] = fmt.Sprintf("// Package shared%d is compiled "+
			"alike by every build.\npackage shared%d\n\n// Value is the package's value.\nconst Value = %d\n", index,
			index, index)
	}
	writeFixtureFiles(test, root, files)
}

// underCapRun runs the engine over root under `--other-builds` and the 8 GiB cap, so the memory, not a fixed rule,
// decides the width, with configure, when not nil, given the pipeline first, and extra after the switches. It returns
// the pipeline, its stream as written and its stderr.
func underCapRun(
	test *testing.T,
	root string,
	wantExit int,
	configure func(*pipeline),
	extra ...string,
) (*pipeline, string, string) {
	test.Helper()
	return runOverNoted(test, root, wantExit, func(engine *pipeline) {
		engine.memory = &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
			Source: contract.MemorySourceCap}}
		if configure != nil {
			configure(engine)
		}
	}, append([]string{"--other-builds"}, extra...)...)
}

// shareGateRun is underCapRun without a findings store, under `--no-cache`, the only run whose width the share gate
// decides (#ghb3rcn).
func shareGateRun(test *testing.T, root string, wantExit int, configure func(*pipeline)) (*pipeline, string, string) {
	test.Helper()
	return underCapRun(test, root, wantExit, configure, "--no-cache")
}

// startPositions is each extra build of engine's described builds by name, at its position in the order the builds
// start in (heaviestFirst).
func startPositions(engine *pipeline) map[string]int {
	builds := engine.described.builds
	positions := map[string]int{}
	for position, index := range heaviestFirst(builds[0], builds[1:]) {
		positions[builds[1+index].String()] = position
	}
	return positions
}

// When every build of a run without a findings store checks at most lowShareAtGate of its packages from source as it
// reaches the share gate, the width is decided there from the host's live heap alone, each build budgeted at
// buildHoldingBudget times it, and no build's holding is measured: the holding the width was given is exactly that
// budget. Each build's record carries what it would check from source at the gate, under the line over the packages its
// load holds (#ghb3rcn).
func TestTheWidthIsTheHostsLiveHeapsWhenEveryShareIsLow(test *testing.T) {
	test.Parallel()
	engine, raw, _ := shareGateRun(test, lowShareBuilds(test), exitFindings, nil)
	if len(engine.buildRuns) != 4 {
		test.Fatalf("the run handed the merge %d runs, want the host's and three beside it", len(engine.buildRuns))
	}
	lint := lintRecordOf(test, raw)
	if !engine.buildsWidthFromHostLive || !lint.BuildsWidthFromHostLive {
		test.Fatalf("the width was decided from the host's live heap %t, recorded %t, want both: every share is low",
			engine.buildsWidthFromHostLive, lint.BuildsWidthFromHostLive)
	}
	budget := uint64(math.Ceil(float64(engine.buildsWidthLiveBytes) * 1.2))
	if engine.buildsWidthLiveBytes == 0 || engine.buildsWidthBuildBytes != budget {
		test.Errorf("the width was given a holding of %d bytes beside a live heap of %d, want 1.2 times the live "+
			"heap, %d, and nothing measured", engine.buildsWidthBuildBytes, engine.buildsWidthLiveBytes, budget)
	}
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, budget, false)
	if engine.buildsAtOnce != want {
		test.Errorf("the builds ran %d at once, want %d, the width the host's live heap gives", engine.buildsAtOnce,
			want)
	}
	for _, build := range lint.Builds[1:] {
		share := float64(build.CheckedFromSourceAtGate) / float64(max(build.PackagesLoaded, 1))
		if build.CheckedFromSourceAtGate == 0 || share > lowShareAtGate {
			test.Errorf("the build %s's record reads %d of %d packages from source at the gate, want some, at most "+
				"%.2f of them", build.Target, build.CheckedFromSourceAtGate, build.PackagesLoaded, lowShareAtGate)
		}
	}
}

// When a build of a run without a findings store will check more than lowShareAtGate of its packages from source as it
// reaches the share gate, the width falls back to the measured rule: the first build passes alone, its holding is
// measured as its load ends and budgeted at buildHoldingBudget times it, and the width is what that decides. The third
// platform of threeExtraBuilds here alone imports net/http, none of whose packages the host's run checked, so its
// record reads its share over the line at the gate.
func TestTheWidthIsMeasuredWhenABuildChecksMuchFromSource(test *testing.T) {
	test.Parallel()
	root := threeExtraBuilds(test)
	third := thirdPlatform()
	writeFixtureFiles(test, root, map[string]string{
		"internal/third/heavy_" + third + ".go": "package third\n\n// net/http is many packages the host's run " +
			"never checked.\nimport _ \"net/http\"\n",
	})
	engine, raw, _ := shareGateRun(test, root, exitFindings, nil)
	lint := lintRecordOf(test, raw)
	if engine.buildsWidthFromHostLive || lint.BuildsWidthFromHostLive {
		test.Fatalf("the width was decided from the host's live heap, want it measured: the third platform checks "+
			"net/http from source:\n%s", raw)
	}
	shares := ""
	thirdOver := false
	for _, build := range lint.Builds[1:] {
		share := float64(build.CheckedFromSourceAtGate) / float64(max(build.PackagesLoaded, 1))
		shares += fmt.Sprintf(" %s:%.3f", build.Target, share)
		if strings.HasPrefix(build.Target, third+"/") && share > lowShareAtGate {
			thirdOver = true
		}
	}
	if !thirdOver {
		test.Errorf("the builds' shares at the gate were%s, want the %s build's over %.2f", shares, third,
			lowShareAtGate)
	}
	if engine.buildsWidthBuildBytes == 0 || engine.buildsWidthLiveBytes == 0 {
		test.Fatalf("the width was given a holding of %d bytes beside a live heap of %d, want both measured",
			engine.buildsWidthBuildBytes, engine.buildsWidthLiveBytes)
	}
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes, false)
	if engine.buildsAtOnce != want {
		test.Errorf("the builds ran %d at once, want %d, the width the measured holding gives", engine.buildsAtOnce,
			want)
	}
}

// A build waiting at the share gate after one that failed is refused there and never type-checks, while a build
// before the failed one still runs, as one at a time it would have (#ghb3rcn). At a width of two the first two builds
// in start order pass the gate and the third waits; the second fails as it ends, and the first ends only once the
// third has been refused or let through, so the only slot the third could take is the failed build's. The notes are
// the first build's and the second's failure, as one at a time.
func TestABuildAfterOneThatFailedIsRefusedAtTheShareGate(test *testing.T) {
	test.Parallel()
	var stepsLock sync.Mutex
	steps := map[int][]shareGateStep{}
	ready := map[int]bool{}
	thirdMoved := make(chan struct{})
	var thirdMoving sync.Once
	var engine *pipeline
	var noted string
	withinBesideWait(test, "the run whose second build fails", func() {
		engine, _, noted = shareGateRun(test, lowShareBuilds(test), exitFindings, func(engine *pipeline) {
			engine.buildsWidthAtGateForced = 2
			positions := startPositions(engine)
			engine.shareGateSteps = func(_ project.Build, position int, step shareGateStep) {
				stepsLock.Lock()
				steps[position] = append(steps[position], step)
				stepsLock.Unlock()
				if position == 2 && (step == shareGateRefused || step == shareGateAdmitted) {
					thirdMoving.Do(func() { close(thirdMoved) })
				}
			}
			engine.lintSteps = func(extra project.Build, step lintStep, _ int) {
				if step == lintStepReady {
					stepsLock.Lock()
					ready[positions[extra.String()]] = true
					stepsLock.Unlock()
				}
			}
			engine.extraBuildEnds = func(extra project.Build, notes io.Writer) error {
				position := positions[extra.String()]
				noteTo(notes, "the planted note of the build started at %d", position)
				switch position {
				case 0:
					select {
					case <-thirdMoved:
					case <-time.After(besideWait):
						return fmt.Errorf("the third build never moved at the share gate")
					}
				case 1:
					return fmt.Errorf("the planted failure")
				}
				return nil
			}
		})
	})
	stepsLock.Lock()
	defer stepsLock.Unlock()
	if !slices.Contains(steps[2], shareGateRefused) || slices.Contains(steps[2], shareGateAdmitted) || ready[2] {
		test.Errorf("the build after the failed one stepped %v at the share gate and reached its lint %t, want "+
			"refused and never", steps[2], ready[2])
	}
	if !slices.Contains(steps[0], shareGateAdmitted) || !ready[0] {
		test.Errorf("the build before the failed one stepped %v at the share gate and reached its lint %t, want "+
			"admitted and linted", steps[0], ready[0])
	}
	if engine.buildRuns != nil {
		test.Errorf("a run whose build failed handed the merge %d runs, want none", len(engine.buildRuns))
	}
	for _, want := range []string{"the planted note of the build started at 0", "the planted note of the build " +
		"started at 1", ": the planted failure"} {
		if !strings.Contains(noted, want) {
			test.Errorf("stderr does not say %q:\n%s", want, noted)
		}
	}
	if strings.Contains(noted, "started at 2") || strings.Contains(noted, "refused") {
		test.Errorf("stderr names the build after the failed one:\n%s", noted)
	}
}

// Builds pass the share gate in the order they started in: a later build is never admitted before an earlier one is
// admitted or ended, even with room for every build at once. The first build is held once it has reported until a
// later build has looked for its turn after the shares were judged, so a later one that took no account of the first
// would pass it there.
func TestBuildsPassTheShareGateInTheOrderTheyStarted(test *testing.T) {
	test.Parallel()
	var orderLock sync.Mutex
	var admitted []int
	laterLooked := make(chan struct{})
	var laterLooking sync.Once
	withinBesideWait(test, "the run held at the share gate", func() {
		shareGateRun(test, lowShareBuilds(test), exitFindings, func(engine *pipeline) {
			engine.buildsWidthAtGateForced = 3
			engine.shareGateSteps = func(_ project.Build, position int, step shareGateStep) {
				switch {
				case step == shareGateAdmitted:
					orderLock.Lock()
					admitted = append(admitted, position)
					orderLock.Unlock()
					if position > 0 {
						laterLooking.Do(func() { close(laterLooked) })
					}
				case step == shareGateHeld && position > 0:
					laterLooking.Do(func() { close(laterLooked) })
				case step == shareGateWaiting && position == 0:
					<-laterLooked
				}
			}
		})
	})
	orderLock.Lock()
	defer orderLock.Unlock()
	if !slices.Equal(admitted, []int{0, 1, 2}) {
		test.Errorf("the builds passed the share gate in the order %v, want 0, 1, 2, the order they started in",
			admitted)
	}
}

// A build that ends before it reaches the share gate, its load failed, arrives there with no share, so the shares are
// still judged once every other build has reported, and the builds after it pass with it settled.
func TestABuildEndedBeforeTheShareGateStillLetsTheSharesBeJudged(test *testing.T) {
	test.Parallel()
	gate := newShareGate(3)
	if gate.end(1) {
		test.Fatal("the first build to end was the last to arrive, want two more")
	}
	if gate.report(0, 1, 100) {
		test.Fatal("the second build to arrive was the last, want one more")
	}
	if !gate.report(2, 5, 100) {
		test.Fatal("the third build to arrive was not the last, want the shares judged: the ended build arrived")
	}
	if gate.overLine() {
		test.Errorf("shares of 0.01 and 0.05 read over the line %.2f", lowShareAtGate)
	}
	gate.open(false)
	gate.admit(0)
	if !gate.turn(2, true) {
		test.Error("the third build waits for the second, which ended before its gate, want its turn")
	}
}

// A run over an empty store takes no share gate and keeps the measured rule whole (#ghb3rcn, #ybjbqm2): its host
// missed every package, a cold fill, and cold with a store the gate cost about 3 CPU-s at cohere's root on Shackler at
// every width tried. Over lowShareBuilds, whose shares the gate would read as low, no build steps at the gate, the
// width is not decided from the host's live heap, and the first build's holding is measured and taken at the store's
// peak factor, while every build's record still carries what it would check from source at the gate.
func TestARunOverAnEmptyStoreTakesNoShareGate(test *testing.T) {
	test.Parallel()
	var stepped atomic.Int64
	engine, raw, _ := underCapRun(test, lowShareBuilds(test), exitFindings, func(engine *pipeline) {
		engine.shareGateSteps = func(project.Build, int, shareGateStep) { stepped.Add(1) }
	})
	if !engine.withFindingsStore() {
		test.Fatal("the run has no findings store, want one: the cache is on")
	}
	lint := lintRecordOf(test, raw)
	if lint.Cache.Missed == 0 || lint.Cache.Hit != 0 {
		test.Fatalf("the host hit %d packages and missed %d, want every package missed: the store is empty",
			lint.Cache.Hit, lint.Cache.Missed)
	}
	if stepped.Load() != 0 || engine.buildsWidthFromHostLive || lint.BuildsWidthFromHostLive {
		test.Errorf("a run over an empty store stepped %d times at the share gate and decided from the host's live "+
			"heap %t, want neither", stepped.Load(), engine.buildsWidthFromHostLive)
	}
	if engine.buildsWidthBuildBytes == 0 || lint.BuildsWidthPeakFactor != buildPeakOverLoadEndWithStore {
		test.Errorf("the width was given a holding of %d bytes at a peak factor of %.1f, want one measured at %.1f",
			engine.buildsWidthBuildBytes, lint.BuildsWidthPeakFactor, buildPeakOverLoadEndWithStore)
	}
	for _, build := range lint.Builds[1:] {
		if build.CheckedFromSourceAtGate == 0 {
			test.Errorf("the build %s's record reads nothing from source at the gate, want its count", build.Target)
		}
	}
}

// A run with a store takes the share gate once its host mostly restored, its own missed share at or under
// lowShareAtGate (#ybjbqm2): over lowShareBuilds and one store, the first run fills it and takes no gate, and a second
// after one leaf package's edit, the host missing that one of many, takes it. Every build there restores nearly all it
// holds, so the width is decided from the host's live heap, at the store's peak factor. A run with every package hit
// is TestAnAllHitWarmRunTakesTheShareGatesWidth's.
func TestARunWithAStoreTakesTheShareGateOnceTheHostMostlyRestored(test *testing.T) {
	test.Parallel()
	root := lowShareBuilds(test)
	// run is one run over root's store, its lint record, and how many steps its builds took at the share gate.
	run := func() (contract.LintRecord, int64) {
		var stepped atomic.Int64
		engine, raw, _ := underCapRun(test, root, exitFindings, func(engine *pipeline) {
			engine.shareGateSteps = func(project.Build, int, shareGateStep) { stepped.Add(1) }
		})
		if !engine.withFindingsStore() {
			test.Fatal("the run has no findings store, want one: the cache is on")
		}
		return lintRecordOf(test, raw), stepped.Load()
	}
	cold, coldSteps := run()
	if cold.Cache.Hit != 0 || coldSteps != 0 || cold.BuildsWidthFromHostLive {
		test.Fatalf("the cold run's host hit %d packages and its builds stepped %d times at the share gate, want "+
			"none and none: the store was empty", cold.Cache.Hit, coldSteps)
	}
	writeFixtureFiles(test, root, map[string]string{
		"internal/shared0/shared.go": "// Package shared0 is compiled alike by every build.\npackage shared0\n\n" +
			"// Value is the package's value, edited.\nconst Value = 100\n",
	})
	edited, editedSteps := run()
	if edited.Cache.Missed != 1 || edited.Cache.Hit == 0 {
		test.Fatalf("the run after a leaf edit hit %d packages at the host and missed %d, want the edited one "+
			"missed alone", edited.Cache.Hit, edited.Cache.Missed)
	}
	if editedSteps == 0 || !edited.BuildsWidthFromHostLive ||
		edited.BuildsWidthPeakFactor != buildPeakOverLoadEndWithStore {
		test.Errorf("the run after a leaf edit, its host missing 1 of %d, stepped %d times at the share gate, its "+
			"width from the host's live heap %t at a peak factor of %.1f, want steps, yes and %.1f",
			edited.Cache.Hit+edited.Cache.Missed, editedSteps, edited.BuildsWidthFromHostLive,
			edited.BuildsWidthPeakFactor, buildPeakOverLoadEndWithStore)
	}
}

// A warm run with a store whose every package hit takes the share gate and the width the host's live heap gives
// there, more than one build at once under the cap (#s1hdd3z): its host hit every package in its store and keeps none
// for its builds, as since #syscache it keeps no standard-library package either, and that alone held every warm run
// at one at a time before the gate read anything. Over lowShareBuilds and one store, the first run fills it, and the
// second, every package hit and nothing kept, steps at the gate and runs its three builds as the host's live heap says.
func TestAnAllHitWarmRunTakesTheShareGatesWidth(test *testing.T) {
	test.Parallel()
	root := lowShareBuilds(test)
	underCapRun(test, root, exitFindings, nil)
	var stepped atomic.Int64
	engine, raw, _ := underCapRun(test, root, exitFindings, func(engine *pipeline) {
		engine.shareGateSteps = func(project.Build, int, shareGateStep) { stepped.Add(1) }
	})
	lint := lintRecordOf(test, raw)
	if lint.Cache.Missed != 0 || lint.Cache.Hit == 0 || len(engine.buildRuns) != 4 ||
		engine.buildRuns[0].result.Host.EntriesKept() != 0 {
		test.Fatalf("the warm run's host hit %d packages and missed %d, keeping %d entries for %d runs beside it, "+
			"want every package hit, none kept, and three builds: the premise", lint.Cache.Hit, lint.Cache.Missed,
			engine.buildRuns[0].result.Host.EntriesKept(), len(engine.buildRuns)-1)
	}
	if stepped.Load() == 0 || !lint.BuildsWidthFromHostLive || lint.BuildsAtOnce <= 1 {
		test.Fatalf("the warm run stepped %d times at the share gate and ran %d builds at once, from the host's live "+
			"heap %t, want steps and more than one at once from the host's live heap", stepped.Load(),
			lint.BuildsAtOnce, lint.BuildsWidthFromHostLive)
	}
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes, true)
	if lint.BuildsAtOnce != want {
		test.Errorf("the warm run ran %d builds at once, want %d, the width the host's live heap gives",
			lint.BuildsAtOnce, want)
	}
}

// A warm run judging each build's dependence takes the measured rule, as on 6a06be201 (#f9br7sg, #s1hdd3z): it takes
// no share gate, and its host hit every package in its store and keeps none, which lets its builds past the guard for
// builds that restore nothing, so the first build's holding is measured and decides the width. Over lowShareBuilds and
// one store under `--build-dependence`, the first run fills it, and the second, every package hit, steps nothing at the
// gate and runs its three builds at the width the first build's measured holding gives, more than one.
func TestAWarmDependenceRunTakesTheMeasuredRule(test *testing.T) {
	test.Parallel()
	root := lowShareBuilds(test)
	underCapRun(test, root, exitFindings, nil, "--build-dependence")
	var stepped atomic.Int64
	engine, raw, _ := underCapRun(test, root, exitFindings, func(engine *pipeline) {
		engine.shareGateSteps = func(project.Build, int, shareGateStep) { stepped.Add(1) }
	}, "--build-dependence")
	lint := lintRecordOf(test, raw)
	if !engine.censusKeepsHostRestoreOff || lint.Cache.Missed != 0 || lint.Cache.Hit == 0 ||
		len(engine.buildRuns) != 4 {
		test.Fatalf("the warm dependence run kept the census's restore off %t, its host hitting %d packages and "+
			"missing %d, with %d runs beside it, want on, every package hit, and three builds: the premise",
			engine.censusKeepsHostRestoreOff, lint.Cache.Hit, lint.Cache.Missed, len(engine.buildRuns)-1)
	}
	if stepped.Load() != 0 || lint.BuildsWidthFromHostLive || lint.BuildsWidthBuildBytes == 0 ||
		lint.BuildsAtOnce <= 1 {
		test.Fatalf("the warm dependence run stepped %d times at the share gate and ran %d builds at once, from the "+
			"host's live heap %t, with a holding of %d bytes, want no step and more than one at once from a measured "+
			"holding", stepped.Load(), lint.BuildsAtOnce, lint.BuildsWidthFromHostLive, lint.BuildsWidthBuildBytes)
	}
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes, true)
	if lint.BuildsAtOnce != want {
		test.Errorf("the warm dependence run ran %d builds at once, want %d, the width the measured holding gives",
			lint.BuildsAtOnce, want)
	}
}

// Only a run whose host mostly hit its findings store lets builds that would restore nothing from the host's run past
// the guard that runs them one at a time (fixedBuildsWidth, #s1hdd3z): over a host that kept nothing, a run whose host
// did not is fixed at one before a test's forced width is read, and one whose host did is given that forced width, as
// builds that restore are. A host mostly hit its store at or under lowShareAtGate's missed share, and never in a run
// without a store, whatever its counts (hostMostlyHitStore).
func TestOnlyAHostThatMostlyHitItsStoreLiftsTheGuard(test *testing.T) {
	test.Parallel()
	engine := &pipeline{
		memory: &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
			Source: contract.MemorySourceCap}},
		buildsWidthForced: 3,
	}
	if width, fixed := engine.fixedBuildsWidth(3, nil, false); width != 1 || !fixed {
		test.Errorf("a run whose host did not mostly hit its store, over a host that kept nothing, was given a width "+
			"of %d, fixed %t, want 1, fixed", width, fixed)
	}
	if width, fixed := engine.fixedBuildsWidth(3, nil, true); width != 3 || !fixed {
		test.Errorf("a run whose host mostly hit its store, over a host that kept nothing, was given a width of %d, "+
			"fixed %t, want the forced 3", width, fixed)
	}
	withStore := &pipeline{session: check.NewCacheSession(&check.Caching{Store: cache.Open(test.TempDir())}, nil)}
	for _, each := range []struct {
		name   string
		engine *pipeline
		counts check.CacheCounts
		want   bool
	}{
		{name: "a store, every package hit", engine: withStore, counts: check.CacheCounts{On: true, Hit: 200},
			want: true},
		{name: "a store, exactly at the line", engine: withStore, counts: check.CacheCounts{On: true, Hit: 90,
			Missed: 10}, want: true},
		{name: "a store, cold", engine: withStore, counts: check.CacheCounts{On: true, Missed: 200}, want: false},
		{name: "no store, nothing missed", engine: &pipeline{}, counts: check.CacheCounts{On: true, Hit: 200},
			want: false},
	} {
		if got := each.engine.hostMostlyHitStore(each.counts); got != each.want {
			test.Errorf("%s: the host mostly hit its store %t, want %t", each.name, got, each.want)
		}
	}
}

// The share gate's line for a run with a store is the host's own missed share, its session's misses over the packages
// its store hit or missed, the system cache's table left out, at or under lowShareAtGate (#ybjbqm2), the same line
// every build's share at the gate is read against: warm, after a leaf edit, and exactly at the line it takes the gate,
// and just over it, or cold, a small tree's among them whose packages from the system cache's table outnumber its
// misses, it keeps main's schedule. A run without a store takes it whatever the counts.
func TestTheHostsMissedShareDecidesTheShareGateUnderAStore(test *testing.T) {
	test.Parallel()
	withStore := &pipeline{session: check.NewCacheSession(&check.Caching{Store: cache.Open(test.TempDir())}, nil)}
	if !withStore.withFindingsStore() {
		test.Fatal("the engine has no findings store, want one")
	}
	for _, each := range []struct {
		name   string
		counts check.CacheCounts
		want   bool
	}{
		{name: "warm, every package hit", counts: check.CacheCounts{On: true, Hit: 200}, want: true},
		{name: "a leaf edit, one of many missed", counts: check.CacheCounts{On: true, Hit: 199, Missed: 1},
			want: true},
		{name: "exactly at the line", counts: check.CacheCounts{On: true, Hit: 90, Missed: 10}, want: true},
		{name: "a leaf edit beside the system cache's table, which is left out", counts: check.CacheCounts{On: true,
			Hit: 95, FromSystemCache: 260, Missed: 5}, want: true},
		{name: "just over the line", counts: check.CacheCounts{On: true, Hit: 89, Missed: 11}, want: false},
		{name: "cold, an empty store", counts: check.CacheCounts{On: true, Missed: 200}, want: false},
		{name: "a small tree's cold fill beside the system cache's table", counts: check.CacheCounts{On: true,
			FromSystemCache: 260, Missed: 20}, want: false},
	} {
		if got := withStore.takesShareGate(each.counts); got != each.want {
			test.Errorf("%s, a missed share of %.3f, took the share gate %t, want %t", each.name,
				hostMissedShare(each.counts), got, each.want)
		}
	}
	if !(&pipeline{}).takesShareGate(check.CacheCounts{On: true, Missed: 200}) {
		test.Error("a run without a store whose host missed everything takes no share gate, want it taken")
	}
}

// A build loaded again for a question passes the share gate at once, admitted once (#ghb3rcn): its reload's load
// reaches the gate again holding the place its first admission took, so a gate that judged it anew would count its
// share twice and, at a width of one, wait behind itself for good. Over restore-from-host, whose windows build declines
// two packages at its check and reloads once, with a package a third platform alone compiles for a second extra build
// and sharedPackages more, at a width forced to one, every build is admitted once, and windows' record keeps its first
// load's count at the gate, under its first round's, not its reload's.
func TestAReloadingBuildIsAdmittedAtTheShareGateOnce(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "restore-from-host"))
	third := thirdPlatform()
	writeFixtureFiles(test, root, map[string]string{
		"third/third.go": "// Package third is compiled beside the fixture's.\npackage third\n",
		"third/third_" + third + ".go": "//go:build " + third + "\n\npackage third\n\n// Third is compiled by " +
			"one platform alone.\nconst Third = 3\n",
	})
	writeSharedPackages(test, root)
	var admittedLock sync.Mutex
	admitted := map[int]int{}
	var engine *pipeline
	var raw string
	withinBesideWait(test, "the run whose build reloads at a width of one", func() {
		engine, raw, _ = shareGateRun(test, root, exitFindings, func(engine *pipeline) {
			engine.buildsWidthAtGateForced = 1
			engine.shareGateSteps = func(_ project.Build, position int, step shareGateStep) {
				if step == shareGateAdmitted {
					admittedLock.Lock()
					admitted[position]++
					admittedLock.Unlock()
				}
			}
		})
	})
	if engine.buildsAtOnce != 1 {
		test.Fatalf("the width was %d, want the forced width of one", engine.buildsAtOnce)
	}
	lint := lintRecordOf(test, raw)
	index := slices.IndexFunc(lint.Builds, func(build contract.BuildRecord) bool {
		return strings.HasPrefix(build.Target, "windows/")
	})
	if index < 0 || len(lint.Builds[index].CheckedFromSourceRounds) != 2 {
		test.Fatalf("windows was not loaded twice, the fixture's premise:\n%s", raw)
	}
	windows := lint.Builds[index]
	admittedLock.Lock()
	defer admittedLock.Unlock()
	if len(admitted) != len(lint.Builds)-1 {
		test.Errorf("%d builds were admitted at the share gate, %v, want each of the %d", len(admitted), admitted,
			len(lint.Builds)-1)
	}
	for position, times := range admitted {
		if times != 1 {
			test.Errorf("the build at %d was admitted %d times, want once", position, times)
		}
	}
	first, reload := windows.CheckedFromSourceRounds[0], windows.CheckedFromSourceRounds[1]
	if windows.CheckedFromSourceAtGate == 0 || windows.CheckedFromSourceAtGate > first {
		test.Errorf("windows' record reads %d from source at the gate, want its first load's, at most its first "+
			"round's %d (its reload checked %d)", windows.CheckedFromSourceAtGate, first, reload)
	}
}

// A build's wait at the share gate is its own figure, shareGateWaitMilliseconds, and its load time leaves it out
// (#ghb3rcn), so load times read alike whatever a build's position. At a width of one the first build holds itself at
// its end until the second waits behind it at the gate, so every build after the first waits there: each build's
// record splits its loads' whole wall time, as the engine told it (buildLoadTimes), into loadMilliseconds and
// shareGateWaitMilliseconds, which sum to it within the millisecond each figure's rounding down may lose, and every
// build after the first was told a wait. The wait is read from what the engine told, not the record's milliseconds,
// since a wait under one rounds to none there.
func TestABuildsWaitAtTheShareGateIsNotItsLoadTime(test *testing.T) {
	test.Parallel()
	secondHeld := make(chan struct{})
	var secondHolding sync.Once
	var wallsLock sync.Mutex
	walls := map[string]time.Duration{}
	waits := map[string]time.Duration{}
	var engine *pipeline
	var raw string
	withinBesideWait(test, "the run whose first build holds the rest at the share gate", func() {
		engine, raw, _ = shareGateRun(test, lowShareBuilds(test), exitFindings, func(engine *pipeline) {
			engine.buildsWidthAtGateForced = 1
			positions := startPositions(engine)
			engine.shareGateSteps = func(_ project.Build, position int, step shareGateStep) {
				if position == 1 && step == shareGateHeld {
					secondHolding.Do(func() { close(secondHeld) })
				}
			}
			engine.buildLoadTimes = func(extra project.Build, loadWall time.Duration, shareGateWait time.Duration) {
				wallsLock.Lock()
				walls[extra.String()], waits[extra.String()] = loadWall, shareGateWait
				wallsLock.Unlock()
			}
			engine.extraBuildEnds = func(extra project.Build, _ io.Writer) error {
				if positions[extra.String()] != 0 {
					return nil
				}
				select {
				case <-secondHeld:
				case <-time.After(besideWait):
					return fmt.Errorf("the second build never waited at the share gate")
				}
				return nil
			}
		})
	})
	if engine.buildsAtOnce != 1 {
		test.Fatalf("the width was %d, want the forced width of one", engine.buildsAtOnce)
	}
	positions := startPositions(engine)
	wallsLock.Lock()
	defer wallsLock.Unlock()
	for _, build := range lintRecordOf(test, raw).Builds[1:] {
		wall, told := walls[build.Target]
		if !told {
			test.Errorf("the engine told no load time of the build %s", build.Target)
			continue
		}
		split := build.LoadMilliseconds + build.ShareGateWaitMilliseconds
		if whole := int(wall.Milliseconds()); split != whole && split != whole-1 {
			test.Errorf("the build %s recorded a load of %d ms and a wait at the share gate of %d ms, %d together, "+
				"want its loads' %d ms whole, within the millisecond rounding may lose", build.Target,
				build.LoadMilliseconds, build.ShareGateWaitMilliseconds, split, whole)
		}
		if positions[build.Target] > 0 && waits[build.Target] <= 0 {
			test.Errorf("the build %s, held behind the first at the share gate, was told no wait there",
				build.Target)
		}
	}
}

// A run judging each build's dependence takes no share gate even without a findings store (#ghb3rcn): its builds
// restore nothing from the host's run for the facts census, so every share would be high and the gate would only hold
// every build before falling back. Under `--build-dependence --no-cache` over lowShareBuilds no build steps at the
// gate, and the width is the first build's measured holding, unbudgeted, at the factor without a store.
func TestADependenceRunTakesNoShareGate(test *testing.T) {
	test.Parallel()
	var stepped atomic.Int64
	engine, raw, _ := underCapRun(test, lowShareBuilds(test), exitFindings, func(engine *pipeline) {
		engine.shareGateSteps = func(project.Build, int, shareGateStep) { stepped.Add(1) }
	}, "--no-cache", "--build-dependence")
	if engine.withFindingsStore() || !engine.censusKeepsHostRestoreOff {
		test.Fatalf("the run has a store %t and keeps the census's restore off %t, want none and on",
			engine.withFindingsStore(), engine.censusKeepsHostRestoreOff)
	}
	lint := lintRecordOf(test, raw)
	if stepped.Load() != 0 || lint.BuildsWidthFromHostLive || lint.BuildsWidthBuildBytes == 0 {
		test.Errorf("a dependence run stepped %d times at the share gate, decided from the host's live heap %t, "+
			"with a holding of %d bytes, want no step, no and a measured holding", stepped.Load(),
			lint.BuildsWidthFromHostLive, lint.BuildsWidthBuildBytes)
	}
}
