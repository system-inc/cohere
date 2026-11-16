package go_engine

import (
	"fmt"
	"io"
	"math"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

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
	files := map[string]string{}
	for index := range sharedPackages {
		files[fmt.Sprintf("internal/shared%d/shared.go", index)] = fmt.Sprintf("// Package shared%d is compiled "+
			"alike by every build.\npackage shared%d\n\n// Value is the package's value.\nconst Value = %d\n", index,
			index, index)
	}
	writeFixtureFiles(test, root, files)
	return root
}

// shareGateRun runs the engine over root with the findings cache on, cold, under `--other-builds` and the 8 GiB cap,
// so the memory, not a fixed rule, decides the width at the share gate, with configure, when not nil, given the
// pipeline first. It returns the pipeline, its stream as written and its stderr.
func shareGateRun(test *testing.T, root string, wantExit int, configure func(*pipeline)) (*pipeline, string, string) {
	test.Helper()
	return runOverNoted(test, root, wantExit, func(engine *pipeline) {
		engine.memory = &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
			Source: contract.MemorySourceCap}}
		if configure != nil {
			configure(engine)
		}
	}, "--other-builds")
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

// When every build checks at most lowShareAtGate of its packages from source as it reaches the share gate, the width
// is decided there from the host's live heap alone, each build budgeted at buildHoldingBudget times it, and no build's
// holding is measured: the holding the width was given is exactly that budget. Each build's record carries what it
// would check from source at the gate, under the line over the packages its load holds (#ghb3rcn).
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
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, budget, true)
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

// When a build will check more than lowShareAtGate of its packages from source as it reaches the share gate, the
// width falls back to the measured rule: the first build passes alone, its holding is measured as its load ends and
// budgeted at buildHoldingBudget times it, and the width is what that decides. The third platform of threeExtraBuilds
// here alone imports net/http, none of whose packages the host's run checked, so its record reads its share over the
// line at the gate.
func TestTheWidthIsMeasuredWhenABuildChecksMuchFromSource(test *testing.T) {
	test.Parallel()
	root := threeExtraBuilds(test)
	third := ""
	for _, operatingSystem := range []string{"linux", "darwin", "freebsd"} {
		if operatingSystem != runtime.GOOS {
			third = operatingSystem
			break
		}
	}
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
	want, _ := buildsWidth(3, false, engine.memory, engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes, true)
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
