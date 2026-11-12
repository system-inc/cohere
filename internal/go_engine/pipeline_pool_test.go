package go_engine

import (
	"strings"
	"sync"
	"testing"

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
