package go_engine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// besideWait is how long a planted build end waits for the builds it waits on before it calls the run broken, far
// past what the fixture's builds take, so only builds that never ran beside it reach it.
const besideWait = 2 * time.Minute

// threeExtraBuilds is a copy of the other-builds fixture with a package one more platform alone compiles beside the
// tagged build and windows, the first of linux, darwin and freebsd that is not the host's, so a run under
// `--other-builds` checks three builds beside the host's.
func threeExtraBuilds(test *testing.T) string {
	test.Helper()
	third := ""
	for _, operatingSystem := range []string{"linux", "darwin", "freebsd"} {
		if operatingSystem != runtime.GOOS {
			third = operatingSystem
			break
		}
	}
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	files := map[string]string{
		"third.go": "// Package third is compiled beside the fixture's.\npackage third\n",
		"third_" + third + ".go": "//go:build " + third +
			"\n\npackage third\n\n// Third is compiled by one platform alone.\nconst Third = 3\n",
	}
	for name, contents := range files {
		path := filepath.Join(root, "internal", "third", name)
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	return root
}

// plantedEnds is what a test plants at the end of each extra build of a run at width (extraBuildEnds): every build
// notes a line naming its place, and the build at failing, counted from 0 among the extra builds, fails, -1 for none.
// Above width 1 each build waits before it ends for the builds waitsOn names, by place, so the builds end out of build
// order: a build that waits on another can only end once that one has, which needs them to run side by side.
type plantedEnds struct {
	width   int
	failing int
	waitsOn map[int][]int
}

// configure has engine run its extra builds at the planted width, each ending as planted.
//
// Mutates: engine gains the forced width and the planted build ends
func (planted plantedEnds) configure(engine *pipeline) {
	engine.buildsWidthForced = planted.width
	ended := map[int]chan struct{}{}
	for place := range len(engine.described.builds) {
		ended[place] = make(chan struct{})
	}
	engine.extraBuildEnds = func(extra project.Build, notes io.Writer) error {
		place := slices.IndexFunc(engine.described.builds[1:], func(build project.Build) bool {
			return build.String() == extra.String()
		})
		defer close(ended[place])
		if planted.width > 1 {
			for _, waited := range planted.waitsOn[place] {
				select {
				case <-ended[waited]:
				case <-time.After(besideWait):
					return fmt.Errorf("build %d never ended beside build %d", waited, place)
				}
			}
		}
		noteTo(notes, "the planted note of extra build %d, %s", place, extra)
		if place == planted.failing {
			return errors.New("the planted failure")
		}
		return nil
	}
}

// runPlanted runs the engine over a fresh copy of threeExtraBuilds under `--no-cache` and `--other-builds` with
// planted, and returns the pipeline, its stream normalized, its stderr with the root read as /project, and its stream
// as written.
func runPlanted(test *testing.T, planted plantedEnds, wantExit int) (*pipeline, string, string, string) {
	test.Helper()
	root := threeExtraBuilds(test)
	engine, stream, noted := runOverNoted(test, root, wantExit, planted.configure, "--no-cache", "--other-builds")
	return engine, normalize(stream, root), strings.ReplaceAll(noted, root, "/project"), stream
}

// Extra builds run side by side write what they write one at a time, byte for byte, the notes on stderr included
// (#b2t9xmf): the three builds beside the host's of the fixture run at width 1 and at width 3, where the first ends
// last, after the other two, and every build notes. The merge is handed the runs in build order, the notes are written
// in build order, and only the lint record's buildsAtOnce, which normalize leaves out, says the width.
func TestExtraBuildsSideBySideWriteWhatOneAtATimeWrites(test *testing.T) {
	test.Parallel()
	ends := map[int]plantedEnds{}
	for _, width := range []int{1, 3} {
		ends[width] = plantedEnds{width: width, failing: -1, waitsOn: map[int][]int{0: {1, 2}}}
	}
	oneEngine, oneStream, oneNoted, _ := runPlanted(test, ends[1], exitFindings)
	threeEngine, threeStream, threeNoted, threeRaw := runPlanted(test, ends[3], exitFindings)

	if len(oneEngine.buildRuns) != 4 || len(threeEngine.buildRuns) != 4 {
		test.Fatalf("the merge was handed %d and %d runs, want the host's and three beside it in each",
			len(oneEngine.buildRuns), len(threeEngine.buildRuns))
	}
	if oneEngine.buildsAtOnce != 1 || threeEngine.buildsAtOnce != 3 {
		test.Errorf("the builds ran %d and %d at once, want 1 and 3", oneEngine.buildsAtOnce, threeEngine.buildsAtOnce)
	}
	if !strings.Contains(threeRaw, `"buildsAtOnce":3`) {
		test.Errorf("the lint record of the run at width 3 does not say it:\n%s", threeRaw)
	}
	for place := range 3 {
		if !strings.Contains(oneNoted, fmt.Sprintf("the planted note of extra build %d,", place)) {
			test.Errorf("the run one at a time noted nothing of extra build %d:\n%s", place, oneNoted)
		}
	}
	if threeStream != oneStream {
		test.Errorf("the stream at width 3 differs from one at a time's:\nwidth 3\n%s\nwidth 1\n%s", threeStream,
			oneStream)
	}
	if threeNoted != oneNoted {
		test.Errorf("stderr at width 3 differs from one at a time's:\nwidth 3\n%s\nwidth 1\n%s", threeNoted, oneNoted)
	}
}

// When the second of three extra builds fails, a run one at a time notes the first two and why the second failed,
// never starts the third, and is incomplete with no build merged. Side by side the third has already ended, its note
// written into its own buffer, by the time the second fails, since the second waits for it: its run and its note are
// dropped, so the run writes, notes and exits as one at a time does (#b2t9xmf).
func TestAFailingMiddleBuildWritesWhatOneAtATimeWrites(test *testing.T) {
	test.Parallel()
	ends := map[int]plantedEnds{}
	for _, width := range []int{1, 3} {
		ends[width] = plantedEnds{width: width, failing: 1, waitsOn: map[int][]int{1: {2}}}
	}
	oneEngine, oneStream, oneNoted, _ := runPlanted(test, ends[1], exitFindings)
	threeEngine, threeStream, threeNoted, _ := runPlanted(test, ends[3], exitFindings)

	if oneEngine.buildRuns != nil || threeEngine.buildRuns != nil {
		test.Errorf("a run whose build failed handed the merge %d and %d runs, want none", len(oneEngine.buildRuns),
			len(threeEngine.buildRuns))
	}
	if !strings.Contains(oneNoted, "the builds beside the host's were not checked: the build ") ||
		!strings.Contains(oneNoted, ": the planted failure") {
		test.Errorf("the run one at a time did not say why the builds were not checked:\n%s", oneNoted)
	}
	for _, noted := range []string{oneNoted, threeNoted} {
		if strings.Contains(noted, "the planted note of extra build 2,") {
			test.Errorf("the note of the build after the one that failed was written:\n%s", noted)
		}
	}
	if !strings.Contains(oneStream, `"complete":false`) {
		test.Errorf("the run whose build failed reads complete:\n%s", oneStream)
	}
	if threeStream != oneStream {
		test.Errorf("the stream at width 3 differs from one at a time's:\nwidth 3\n%s\nwidth 1\n%s", threeStream,
			oneStream)
	}
	if threeNoted != oneNoted {
		test.Errorf("stderr at width 3 differs from one at a time's:\nwidth 3\n%s\nwidth 1\n%s", threeNoted, oneNoted)
	}
}

// The width reaches no key (#b2t9xmf): a run at width 3 over the tree a run at width 1 just checked, with the findings
// cache on, writes the inputs record the front door's run cache replays by, byte for byte, and restores every package
// of every build from the findings cache the first run wrote, with the same verdict. The builds check every package
// themselves, so the counts are each build's own store's alone.
func TestTheBuildsWidthReachesNoKey(test *testing.T) {
	test.Parallel()
	root := threeExtraBuilds(test)
	configure := func(width int) func(*pipeline) {
		return func(engine *pipeline) {
			engine.keepsHostRestoreOff = true
			engine.buildsWidthForced = width
		}
	}
	one, oneRaw := runOverConfigured(test, root, exitFindings, configure(1), "--other-builds")
	three, threeRaw := runOverConfigured(test, root, exitFindings, configure(3), "--other-builds")
	if len(one.buildRuns) != 4 || len(three.buildRuns) != 4 || three.buildsAtOnce != 3 {
		test.Fatalf("the runs handed the merge %d and %d runs, the second %d at once, want 4 each and 3 at once",
			len(one.buildRuns), len(three.buildRuns), three.buildsAtOnce)
	}
	inputsLine := func(stream string) string {
		for line := range strings.Lines(stream) {
			if strings.HasPrefix(line, `{"kind":"inputs"`) {
				return line
			}
		}
		test.Fatalf("no inputs record in\n%s", stream)
		return ""
	}
	if inputsLine(threeRaw) != inputsLine(oneRaw) {
		test.Errorf("the inputs record at width 3 differs from width 1's, so the run cache would not replay it:\n"+
			"width 3 %s\nwidth 1 %s", inputsLine(threeRaw), inputsLine(oneRaw))
	}
	for index, run := range three.buildRuns {
		first, second := one.buildRuns[index].result.Cache, run.result.Cache
		if second.Hit != first.Hit+first.Missed || second.Missed != 0 || second.Written != 0 {
			test.Errorf("%s at width 3 hit %d, missed %d and wrote %d, want every one of the %d width 1 keyed hit",
				run.build, second.Hit, second.Missed, second.Written, first.Hit+first.Missed)
		}
	}
	if withoutCache(normalize(threeRaw, root)) != withoutCache(normalize(oneRaw, root)) {
		test.Errorf("the run at width 3 changed the verdict:\nwidth 3\n%s\nwidth 1\n%s", threeRaw, oneRaw)
	}
}

// Extra builds side by side load side by side, and their loads share the run's one memory limit (loadMemory): over the
// fixture at width 3 with the limit watched, every load runs and ends under the cap, a raise landing only once no load
// is in flight, and no limit the run sets ever passes the cap plus lateMemoryAllowance, the run's worst case (#44n0vap,
// #b2t9xmf).
func TestBuildsSideBySideNeverPassTheCapPlusOneAllowance(test *testing.T) {
	test.Parallel()
	watch := &memoryWatch{current: engineMemoryCap}
	// during is the limit at every look inside a load, as package load asks for a file's held bytes.
	var during []int64
	configure := func(engine *pipeline) {
		engine.memory = capMemoryWatched(watch, watch.collecting)
		engine.memory.duringLoad = func() {
			watch.lock.Lock()
			defer watch.lock.Unlock()
			during = append(during, watch.current)
		}
		engine.loads = newLoadMemory(engine.memory, false)
		engine.buildsWidthForced = 3
	}
	engine, _ := runOverConfigured(test, threeExtraBuilds(test), exitFindings, configure, "--no-cache",
		"--other-builds")
	if engine.buildsAtOnce != 3 || len(engine.buildRuns) != 4 {
		test.Fatalf("the run ran %d builds beside the host's, %d at once, want 3 at once", len(engine.buildRuns)-1,
			engine.buildsAtOnce)
	}
	ceiling := engineMemoryCap + lateMemoryAllowance
	watch.lock.Lock()
	defer watch.lock.Unlock()
	if !watch.raised || len(watch.endedUnder) == 0 {
		test.Fatalf("the run raised %t and ended %d loads, want a raise after a load", watch.raised,
			len(watch.endedUnder))
	}
	for _, limit := range watch.set {
		if limit > ceiling {
			test.Errorf("the run set its limit to %d, past the cap plus one allowance, %d", limit, ceiling)
		}
	}
	if len(during) == 0 {
		test.Fatal("no look inside a load read the limit, so what the loads ran under went unread")
	}
	for index, limit := range during {
		if limit != engineMemoryCap {
			test.Errorf("look %d of %d inside a load read a limit of %d, want the cap, %d", index+1, len(during),
				limit, engineMemoryCap)
		}
	}
	for index, limit := range watch.endedUnder {
		if limit != engineMemoryCap {
			test.Errorf("load %d of %d ended under a limit of %d, want the cap, %d", index+1, len(watch.endedUnder),
				limit, engineMemoryCap)
		}
	}
}

// The width is the fewest of the extra builds, a quarter of the cores and how many builds the memory cap holds beside
// the live heap, with at least one, and one at a time under `--single-threaded`, without a memory decision or a live
// heap read, and under any limit but the cap.
func TestTheBuildsWidth(test *testing.T) {
	test.Parallel()
	underCap := &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
		Source: contract.MemorySourceCap}}
	underSource := func(source contract.MemorySource) *engineMemory {
		return &engineMemory{record: contract.Memory{LimitBytes: engineMemoryCap, CapBytes: engineMemoryCap,
			Source: source}}
	}
	const gibibyte = uint64(1) << 30
	for _, each := range []struct {
		name           string
		extras         int
		processors     int
		singleThreaded bool
		memory         *engineMemory
		live           uint64
		want           int
	}{
		{name: "memory holds three", extras: 6, processors: 16, memory: underCap, live: 2 * gibibyte, want: 3},
		{name: "cores hold four", extras: 6, processors: 16, memory: underCap, live: gibibyte, want: 4},
		{name: "builds are two", extras: 2, processors: 16, memory: underCap, live: gibibyte, want: 2},
		{name: "cores hold two", extras: 6, processors: 8, memory: underCap, live: gibibyte, want: 2},
		{name: "cores hold none", extras: 6, processors: 2, memory: underCap, live: gibibyte, want: 1},
		{name: "a tiny live heap", extras: 6, processors: 16, memory: underCap, live: 1, want: 4},
		{name: "the live heap past the cap", extras: 6, processors: 16, memory: underCap, live: 9 * gibibyte, want: 1},
		{name: "single-threaded", extras: 6, processors: 16, singleThreaded: true, memory: underCap,
			live: gibibyte, want: 1},
		{name: "no memory decision", extras: 6, processors: 16, live: gibibyte, want: 1},
		{name: "no live heap read", extras: 6, processors: 16, memory: underCap, want: 1},
		{name: "half the memory available", extras: 6, processors: 16,
			memory: underSource(contract.MemorySourceAvailable), live: gibibyte, want: 1},
		{name: "the environment's limit", extras: 6, processors: 16,
			memory: underSource(contract.MemorySourceEnvironment), live: gibibyte, want: 1},
		{name: "no memory read", extras: 6, processors: 16, memory: underSource(contract.MemorySourceUnreadable),
			live: gibibyte, want: 1},
	} {
		got := buildsWidth(each.extras, each.processors, each.singleThreaded, each.memory, each.live)
		if got != each.want {
			test.Errorf("%s: the width is %d, want %d", each.name, got, each.want)
		}
	}
}
