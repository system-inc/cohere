package go_engine

import (
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// The run's one pool bounds the work of every build at once (#ckm0hsp): over the fixture's three builds beside the
// host's at width 3, each held once its load has ended until all three have, so their rules start together, with a
// pool of two slots, the run makes one pool, the slots held at once across the host and every build never pass two,
// and the lint record says the pool's size.
func TestEveryBuildSharesTheRunsOnePool(test *testing.T) {
	test.Parallel()

	var lock sync.Mutex
	held, peak, ready := 0, 0, 0
	allReady := make(chan struct{})
	engine, raw := runOverConfigured(test, threeExtraBuilds(test), exitFindings, func(engine *pipeline) {
		engine.buildsWidthForced = 3
		engine.poolSizeForced = 2
		engine.poolObserve = func(change int) {
			lock.Lock()
			defer lock.Unlock()
			held += change
			peak = max(peak, held)
		}
		engine.lintSteps = func(_ project.Build, step lintStep, _ int) {
			if step != lintStepReady {
				return
			}
			lock.Lock()
			ready++
			if ready == 3 {
				close(allReady)
			}
			lock.Unlock()
			awaitStep(test, allReady, "every build's load ending")
		}
	}, "--no-cache", "--other-builds")
	if engine.buildsAtOnce != 3 {
		test.Fatalf("the builds ran %d at once, want 3, so the pool is shared by builds side by side",
			engine.buildsAtOnce)
	}
	if engine.poolsMade != 1 {
		test.Errorf("the run made %d pools, want one, which every load and check shares", engine.poolsMade)
	}
	if peak > 2 || peak == 0 {
		test.Errorf("at most %d of the pool's slots were held at once across every build, want 2, its size, or fewer, "+
			"and some", peak)
	}
	if !strings.Contains(raw, `"buildsPoolSize":2`) {
		test.Errorf("the lint record does not say the pool's size:\n%s", raw)
	}
}

// Each extra build's load and rules take their slots from the run's one pool (#ckm0hsp): with the fixture's three
// builds one at a time, each build's rules, from the weight it takes at the lint gate to the weight it gives back, and
// the load of each build after the first, from the end of the build before it to the end of its load, take slots of
// the pool. A build whose load or check ran on slots of its own would take none there. The first build's load is not
// counted, since nothing marks where the host's work ends and its own begins.
func TestEachBuildsLoadAndRulesTakeThePoolsSlots(test *testing.T) {
	test.Parallel()

	var lock sync.Mutex
	taken := 0
	type window struct {
		load     int
		rules    int
		measured bool
	}
	windows := map[string]*window{}
	mark, previousEnded := 0, false
	engine, _ := runOverConfigured(test, threeExtraBuilds(test), exitFindings, func(engine *pipeline) {
		engine.buildsWidthForced = 1
		engine.poolObserve = func(change int) {
			lock.Lock()
			defer lock.Unlock()
			if change > 0 {
				taken++
			}
		}
		engine.lintSteps = func(extra project.Build, step lintStep, _ int) {
			lock.Lock()
			defer lock.Unlock()
			name := extra.String()
			if windows[name] == nil {
				windows[name] = &window{}
			}
			switch step {
			case lintStepReady:
				windows[name].load, windows[name].measured = taken-mark, previousEnded
			case lintStepTaken:
				mark = taken
			case lintStepGiving:
				windows[name].rules = taken - mark
				mark, previousEnded = taken, true
			}
		}
	}, "--no-cache", "--other-builds")
	if engine.buildsAtOnce != 1 || len(windows) != 3 {
		test.Fatalf("%d builds ran %d at once, want 3, one at a time", len(windows), engine.buildsAtOnce)
	}
	measured := 0
	for name, each := range windows {
		if each.measured {
			measured++
		}
		if each.measured && each.load == 0 || each.rules == 0 {
			test.Errorf("%s took %d of the pool's slots in its load (counted %t) and %d in its rules, want some in "+
				"each counted", name, each.load, each.measured, each.rules)
		}
	}
	if measured != 2 {
		test.Errorf("%d builds' loads were counted, want the two after the first", measured)
	}
}

// The width changes no finding under the one pool: the fixture's builds beside the host's forced one at a time and
// four at once, which three builds hold to three, write the same stream but for the lint record's width, which
// normalize leaves out.
func TestTheWidthChangesNothingUnderTheOnePool(test *testing.T) {
	test.Parallel()

	streams := map[int]string{}
	for _, width := range []int{1, 4} {
		root := threeExtraBuilds(test)
		_, stream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
			engine.buildsWidthForced = width
		}, "--no-cache", "--other-builds")
		streams[width] = normalize(stream, root)
	}
	if streams[1] != streams[4] {
		test.Errorf("the stream at width 4 differs from one at a time's:\nwidth 4\n%s\nwidth 1\n%s", streams[4],
			streams[1])
	}
}

// The width the memory decides is measured as the first build's load ends (#rmzehfg): over the fixture's three builds
// beside the host's under the memory cap, the first build runs alone to its load's end, its holding is read, the lint
// record names the host's live heap, the holding and the peak factor the width was decided by, 2.5 under `--no-cache`
// and 3.5 with a findings store (#hv090f4), and the stream is the one a run one build at a time writes, but for the
// width.
func TestTheWidthIsMeasuredAsTheFirstBuildsLoadEnds(test *testing.T) {
	test.Parallel()

	underCap := func() *engineMemory {
		return &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
			Source: contract.MemorySourceCap}}
	}
	measured := threeExtraBuilds(test)
	engine, raw := runOverConfigured(test, measured, exitFindings, func(engine *pipeline) {
		engine.memory = underCap()
	}, "--no-cache", "--other-builds")
	if engine.buildsWidthBuildBytes == 0 || engine.buildsWidthLiveBytes == 0 || engine.buildsAtOnce < 1 {
		test.Fatalf("the width %d was decided by a live heap of %d and a holding of %d, want both measured",
			engine.buildsAtOnce, engine.buildsWidthLiveBytes, engine.buildsWidthBuildBytes)
	}
	for _, field := range []string{`"buildsWidthLiveBytes":`, `"buildsWidthBuildBytes":`,
		`"buildsWidthPeakFactor":2.5`} {
		if !strings.Contains(raw, field) {
			test.Errorf("the lint record names no %s the width was decided by:\n%s", field, raw)
		}
	}
	stored := threeExtraBuilds(test)
	_, storedStream := runOverConfigured(test, stored, exitFindings, func(engine *pipeline) {
		engine.memory = underCap()
	}, "--other-builds")
	if !strings.Contains(storedStream, `"buildsWidthPeakFactor":3.5`) {
		test.Errorf("a run with a findings store names no peak factor of 3.5:\n%s", storedStream)
	}
	one := threeExtraBuilds(test)
	_, oneStream := runOverConfigured(test, one, exitFindings, func(engine *pipeline) {
		engine.memory = underCap()
		engine.buildsWidthForced = 1
	}, "--no-cache", "--other-builds")
	if normalize(raw, measured) != normalize(oneStream, one) {
		test.Errorf("the stream at the measured width differs from one at a time's:\nmeasured\n%s\nwidth 1\n%s",
			normalize(raw, measured), normalize(oneStream, one))
	}
}

// Under `--timing` every extra build runs alone, and its record writes what it held beyond the host's live heap as
// its load ended and the most it held while its rules ran, the ratio the width's factor stands for, so a tree that
// breaks the factor shows in one run (#rmzehfg); the host's record writes neither. The builds start on another
// platform first, as every run's do.
func TestTimingRecordsEachBuildsHoldings(test *testing.T) {
	test.Parallel()
	root := threeExtraBuilds(test)
	var startedLock sync.Mutex
	var started []string
	engine, raw := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.lintSteps = func(extra project.Build, step lintStep, _ int) {
			if step == lintStepReady {
				startedLock.Lock()
				started = append(started, extra.String())
				startedLock.Unlock()
			}
		}
	}, "--no-cache", "--other-builds", "--timing")
	startedLock.Lock()
	defer startedLock.Unlock()
	if engine.buildsAtOnce != 1 {
		test.Errorf("the builds ran %d at once under --timing, want one at a time", engine.buildsAtOnce)
	}
	var lint contract.LintRecord
	for line := range strings.SplitSeq(raw, "\n") {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			if decodeError := json.Unmarshal([]byte(line), &lint); decodeError != nil {
				test.Fatal(decodeError)
			}
		}
	}
	if len(lint.Builds) < 3 {
		test.Fatalf("the lint record holds %d builds, want the host's and at least two more:\n%s", len(lint.Builds),
			raw)
	}
	for _, build := range lint.Builds {
		if build.Host {
			if build.HeldAtLoadEndBytes != 0 || build.HeldAtLintPeakBytes != 0 {
				test.Errorf("the host's record holds %d and %d bytes, want neither", build.HeldAtLoadEndBytes,
					build.HeldAtLintPeakBytes)
			}
			continue
		}
		if build.HeldAtLoadEndBytes == 0 || build.HeldAtLintPeakBytes < build.HeldAtLoadEndBytes {
			test.Errorf("the build %s held %d bytes as its load ended and %d at its lint's peak, want both, the "+
				"peak no less", build.Target, build.HeldAtLoadEndBytes, build.HeldAtLintPeakBytes)
		}
		if want := lintPeakOverLoadEnd(build.HeldAtLoadEndBytes, build.HeldAtLintPeakBytes); want < 1 ||
			build.LintPeakOverLoadEnd != want {
			test.Errorf("the build %s records a ratio of %v, want its peak over its load's end, %v", build.Target,
				build.LintPeakOverLoadEnd, want)
		}
	}
	if len(started) == 0 || strings.HasPrefix(started[0], runtime.GOOS+"/"+runtime.GOARCH) {
		test.Errorf("the builds started in the order %v, want another platform's first", started)
	}
}
