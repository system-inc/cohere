package go_engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"slices"
	"strings"
	"sync"
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
	writeFixtureFiles(test, root, map[string]string{
		"internal/third/third.go": "// Package third is compiled beside the fixture's.\npackage third\n",
		"internal/third/third_" + third + ".go": "//go:build " + third +
			"\n\npackage third\n\n// Third is compiled by one platform alone.\nconst Third = 3\n",
	})
	return root
}

// writeFixtureFiles writes each of files, by its path under root, with the directories it needs.
func writeFixtureFiles(test *testing.T, root string, files map[string]string) {
	test.Helper()
	for name, contents := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if makeError := os.MkdirAll(filepath.Dir(path), 0o755); makeError != nil {
			test.Fatal(makeError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
}

// nearFullReaders is how many packages of nearFullWindows read base's Kind.
const nearFullReaders = 6

// nearFullWindows is threeExtraBuilds with a package base that most of the others read, whose Kind windows declares
// otherwise than every other platform, so the windows build's reads of it decline and its first load checks nearly
// every package of its graph from source, while the tagged build and the third platform each check only the package
// it alone compiles. lightTags more packages each compile a file under a tag of their own, each one more light build.
func nearFullWindows(test *testing.T, lightTags int) string {
	test.Helper()
	root := threeExtraBuilds(test)
	files := map[string]string{
		"internal/base/base.go": "// Package base is what most of the fixture's packages read, its Kind each " +
			"platform's own.\npackage base\n",
		"internal/base/kind.go": "//go:build !windows\n\npackage base\n\n// Kind is a number.\n" +
			"type Kind int\n",
		"internal/base/kind_windows.go": "package base\n\n// Kind is a name on windows.\ntype Kind string\n",
	}
	for index := range nearFullReaders {
		files[fmt.Sprintf("internal/reader%d/reader.go", index)] = fmt.Sprintf("// Package reader%d hands back "+
			"a base.Kind.\npackage reader%d\n\nimport \"example.com/builds/internal/base\"\n\n// Same is kind."+
			"\nfunc Same(kind base.Kind) base.Kind { return kind }\n", index, index)
	}
	for index := range lightTags {
		tag := fmt.Sprintf("gate_light_%d", index)
		directory := fmt.Sprintf("internal/light%d/", index)
		files[directory+"light.go"] = fmt.Sprintf("//go:build !%s\n\n// Package light%d is compiled one way "+
			"without its tag.\npackage light%d\n\n// Value is the package's value.\nconst Value = 1\n", tag, index,
			index)
		files[directory+"light_tagged.go"] = fmt.Sprintf("//go:build %s\n\n// Package light%d is compiled "+
			"another way under its tag.\npackage light%d\n\n// Value is the package's value.\nconst Value = 2\n",
			tag, index, index)
	}
	writeFixtureFiles(test, root, files)
	return root
}

// plantedEnds is what a test plants at the end of each extra build of a run at width (extraBuildEnds): every build
// notes a line naming its place, and the build at failing, counted from 0 among the extra builds, fails, -1 for none.
// Above width 1 each build waits before it ends for the builds waitsOn names, by place, so the builds end out of build
// order: a build that waits on another can only end once that one has, which needs them to run side by side.
//
// steps, when not nil, watches every build's lint at the gate (lintSteps).
type plantedEnds struct {
	width   int
	failing int
	waitsOn map[int][]int
	steps   *gateSteps
}

// configure has engine run its extra builds at the planted width, each ending as planted.
//
// Mutates: engine gains the forced width and the planted build ends
func (planted plantedEnds) configure(engine *pipeline) {
	engine.buildsWidthForced = planted.width
	if planted.steps != nil {
		engine.lintSteps = planted.steps.step
	}
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
//
// The failing build gives its lint's weight back as its rules end, before its end, which waits for the build after it,
// so that build is never left waiting for weight the failing one holds: the run at width 3 ends within besideWait, and
// every weight taken was given back (#b2t9xmf).
func TestAFailingMiddleBuildWritesWhatOneAtATimeWrites(test *testing.T) {
	test.Parallel()
	ends := map[int]plantedEnds{}
	for _, width := range []int{1, 3} {
		ends[width] = plantedEnds{width: width, failing: 1, waitsOn: map[int][]int{1: {2}}, steps: newGateSteps()}
	}
	oneEngine, oneStream, oneNoted, _ := runPlanted(test, ends[1], exitFindings)
	var threeEngine *pipeline
	var threeStream string
	var threeNoted string
	withinBesideWait(test, "the run at width 3 whose middle build fails", func() {
		threeEngine, threeStream, threeNoted, _ = runPlanted(test, ends[3], exitFindings)
	})
	steps := ends[3].steps
	steps.lock.Lock()
	if len(steps.taken) != 3 || steps.given != len(steps.taken) || len(steps.holding) != 0 {
		test.Errorf("at width 3 the builds took weight %d times and gave it back %d times, %v still holding, want "+
			"three each and none", len(steps.taken), steps.given, steps.holding)
	}
	steps.lock.Unlock()

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
			engine.buildsWidthUnguarded = true
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

// The width is as many builds as the memory cap holds beside the host's live heap, each taken at its lint's peak,
// floor((cap - live) / (holding * factor)), the factor 3.5 with a findings store and 2.5 without one (#hv090f4), and
// one more where the slack fits it (TestTheBuildsWidthSlack), no more than the builds and at least one, and one at a
// time under
// `--single-threaded`, without a memory decision, a live heap or a holding read, and under any limit but the cap
// (#rmzehfg). The cap here is 8 GiB.
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
		singleThreaded bool
		memory         *engineMemory
		live           uint64
		holding        uint64
		withoutStore   bool
		want           int
	}{
		// 7 GiB of room over peaks of 3.5 GiB: without the factor, the holding alone would make it seven.
		{name: "each build taken at its peak", extras: 6, memory: underCap, live: gibibyte, holding: gibibyte,
			want: 2},
		// Peaks of 2.625 GiB with a store and 1.875 GiB without one, in 7 GiB of room: two and three by the factors,
		// and without a store the slack's fourth, four builds at 2.16 times 0.75 GiB and the margin filling 6.98 GiB.
		{name: "a store's peak", extras: 6, memory: underCap, live: gibibyte, holding: gibibyte * 3 / 4, want: 2},
		{name: "a peak without a store", extras: 6, memory: underCap, live: gibibyte, holding: gibibyte * 3 / 4,
			withoutStore: true, want: 4},
		{name: "memory holds every build", extras: 6, memory: underCap, live: gibibyte / 2, holding: gibibyte / 4,
			want: 6},
		{name: "builds are two", extras: 2, memory: underCap, live: gibibyte, holding: gibibyte / 4, want: 2},
		{name: "room for one peak", extras: 6, memory: underCap, live: 4 * gibibyte, holding: gibibyte, want: 1},
		{name: "a peak past the room", extras: 6, memory: underCap, live: gibibyte, holding: 3 * gibibyte, want: 1},
		{name: "a tiny holding", extras: 6, memory: underCap, live: gibibyte, holding: 1, want: 6},
		{name: "a holding past any peak", extras: 6, memory: underCap, live: gibibyte, holding: 1 << 63, want: 1},
		{name: "the live heap past the cap", extras: 6, memory: underCap, live: 9 * gibibyte, holding: gibibyte,
			want: 1},
		{name: "single-threaded", extras: 6, singleThreaded: true, memory: underCap, live: gibibyte,
			holding: gibibyte / 4, want: 1},
		{name: "no memory decision", extras: 6, live: gibibyte, holding: gibibyte / 4, want: 1},
		{name: "no live heap read", extras: 6, memory: underCap, holding: gibibyte / 4, want: 1},
		{name: "no holding read", extras: 6, memory: underCap, live: gibibyte, want: 1},
		{name: "half the memory available", extras: 6, memory: underSource(contract.MemorySourceAvailable),
			live: gibibyte, holding: gibibyte / 4, want: 1},
		{name: "the environment's limit", extras: 6, memory: underSource(contract.MemorySourceEnvironment),
			live: gibibyte, holding: gibibyte / 4, want: 1},
		{name: "no memory read", extras: 6, memory: underSource(contract.MemorySourceUnreadable), live: gibibyte,
			holding: gibibyte / 4, want: 1},
	} {
		got, _ := buildsWidth(each.extras, each.singleThreaded, each.memory, each.live, each.holding,
			!each.withoutStore)
		if got != each.want {
			test.Errorf("%s: the width is %d, want %d", each.name, got, each.want)
		}
	}
}

// The slack adds one build past the width the factor gives, never two, only when that one fits by the largest ratio
// measured in the run's mode, 2.16 without a findings store and 3.22 with one, with 512 MiB to spare, and only when the
// width is fewer than the builds (#ewkp7w0). Each bound is met to the byte and missed by one. deeafbf4's measured
// heaps under `--no-cache` give 3 with and without the 9.5 MB more live heap that tipped its width to 2, and cohere
// root's, read by the unlabelled release binary, give 5 under `--no-cache`, the slack's fifth, with about 540 MB to
// spare, and 3 with a store. The cap is 8 GiB but where a case says.
func TestTheBuildsWidthSlack(test *testing.T) {
	test.Parallel()
	underLimit := func(limit uint64) *engineMemory {
		return &engineMemory{record: contract.Memory{LimitBytes: int64(limit), CapBytes: int64(limit),
			Source: contract.MemorySourceCap}}
	}
	const gibibyte = uint64(1) << 30
	// fitting is the room, beyond the live heap, in which builds builds of holding fit by measured with the margin
	// to the byte, as buildsWidth reads it.
	fitting := func(builds int, holding uint64, measured float64) uint64 {
		return uint64(math.Ceil(float64(builds)*float64(holding)*measured)) + buildsWidthSlackMarginBytes
	}
	// Without a store the factor gives two builds of a GiB in 5 to 7.5 GiB of room, which the third's 6.98 GiB
	// meets, and with one in 7 to 10.5 GiB, which the third's 10.16 GiB meets.
	withoutStoreRoom := fitting(3, gibibyte, buildPeakOverLoadEndMeasuredWithoutStore)
	withStoreRoom := fitting(3, gibibyte, buildPeakOverLoadEndMeasuredWithStore)
	for _, each := range []struct {
		name      string
		extras    int
		limit     uint64
		live      uint64
		holding   uint64
		withStore bool
		want      int
		wantSlack bool
	}{
		{name: "a third fits by 2.16 with the margin", extras: 6, limit: gibibyte + withoutStoreRoom, live: gibibyte,
			holding: gibibyte, want: 3, wantSlack: true},
		{name: "a third misses by a byte", extras: 6, limit: gibibyte + withoutStoreRoom - 1, live: gibibyte,
			holding: gibibyte, want: 2},
		{name: "a third fits by 3.22 with the margin", extras: 6, limit: gibibyte + withStoreRoom, live: gibibyte,
			holding: gibibyte, withStore: true, want: 3, wantSlack: true},
		{name: "a third misses by a byte with a store", extras: 6, limit: gibibyte + withStoreRoom - 1,
			live: gibibyte, holding: gibibyte, withStore: true, want: 2},
		{name: "a third is the last build", extras: 3, limit: gibibyte + withoutStoreRoom, live: gibibyte,
			holding: gibibyte, want: 3, wantSlack: true},
		{name: "the width is the builds already", extras: 2, limit: gibibyte + withoutStoreRoom, live: gibibyte,
			holding: gibibyte, want: 2},
		// Six builds of 4 GiB by the factor in 69.75 GiB of room, where eight fit by 2.16 with the margin, 69.62 GiB:
		// the slack adds the seventh alone.
		{name: "never two past", extras: 10, limit: 70*gibibyte + 3*gibibyte/4, live: gibibyte,
			holding: 4 * gibibyte, want: 7, wantSlack: true},
		{name: "deeafbf4 under --no-cache", extras: 8, limit: uint64(engineMemoryCap), live: 569_200_000,
			holding: 1_069_200_000, want: 3},
		{name: "deeafbf4 under --no-cache with 9.5 MB more live", extras: 8, limit: uint64(engineMemoryCap),
			live: 578_700_000, holding: 1_069_200_000, want: 3, wantSlack: true},
		{name: "root under --no-cache", extras: 7, limit: uint64(engineMemoryCap), live: 456_300_000,
			holding: 653_400_000, want: 5, wantSlack: true},
		{name: "root with a store", extras: 7, limit: uint64(engineMemoryCap), live: 456_300_000,
			holding: 653_400_000, withStore: true, want: 3},
		// A phase-labelled throwaway binary inflates root's holding to 701.2 MB, where the fifth build fits by 9.5 MB
		// and misses with 32 MB less room: a knife edge the release binary does not have, so a pair judging the width
		// runs unlabelled binaries.
		{name: "root at a labelled binary's holding", extras: 7, limit: uint64(engineMemoryCap), live: 470_600_000,
			holding: 701_200_000, want: 5, wantSlack: true},
		{name: "root at a labelled binary's holding with 32 MB less room", extras: 7,
			limit: uint64(engineMemoryCap) - 32_000_000, live: 470_600_000, holding: 701_200_000, want: 4},
	} {
		got, slack := buildsWidth(each.extras, false, underLimit(each.limit), each.live, each.holding, each.withStore)
		if got != each.want || slack != each.wantSlack {
			test.Errorf("%s: the width is %d, the slack's %t, want %d, %t", each.name, got, slack, each.want,
				each.wantSlack)
		}
	}
}

// A timed run names, in the lint record and on stderr, each build whose lint peaked at more times what it held as its
// load ended than the largest ratio measured in the run's mode, the build, its ratio and the constant (#ewkp7w0): a
// build at 2.17 under a timed run, which has no store, passes 2.16, and one at 2.16 does not.
func TestTimingNamesABuildPastTheMeasuredPeak(test *testing.T) {
	test.Parallel()
	var standardError strings.Builder
	engine := &pipeline{standardError: &standardError}
	record := contract.LintRecord{Builds: []contract.BuildRecord{
		{Target: "darwin/arm64 cgo", Host: true},
		{Target: "linux/arm64", HeldAtLoadEndBytes: 1000, HeldAtLintPeakBytes: 2170,
			LintPeakOverLoadEnd: lintPeakOverLoadEnd(1000, 2170)},
		{Target: "windows/arm64", HeldAtLoadEndBytes: 1000, HeldAtLintPeakBytes: 2160,
			LintPeakOverLoadEnd: lintPeakOverLoadEnd(1000, 2160)},
	}}
	engine.recordBuildsPeakProblems(&record)
	if len(record.BuildsPeakProblems) != 1 {
		test.Fatalf("the record names %d builds past the measured peak, want linux/arm64 alone: %q",
			len(record.BuildsPeakProblems), record.BuildsPeakProblems)
	}
	problem := record.BuildsPeakProblems[0]
	for _, part := range []string{"linux/arm64", "2.170", "buildPeakOverLoadEndMeasuredWithoutStore", "2.16"} {
		if !strings.Contains(problem, part) {
			test.Errorf("the problem %q does not name %s", problem, part)
		}
	}
	if standardError.String() != "cohere-go: "+problem+"\n" {
		test.Errorf("stderr reads %q, want the problem noted", standardError.String())
	}
}

// The first build on another platform starts first, so the build the width is measured by is a platform's, which
// holds the most, never a tag build on the host's (#rmzehfg), and every other build follows in build order, so the
// host's own light builds start before the other platforms and no two platform builds start side by side ahead of
// them, which stacked their lint peaks (#awbzy9y).
func TestTheBuildsStartHeaviestFirst(test *testing.T) {
	test.Parallel()
	host := project.Build{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true}
	// In the order BuildsFor gives them: the host's platform first, then the others.
	extras := []project.Build{
		{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Tags: []string{"first"}},
		{OperatingSystem: "darwin", Architecture: "arm64"},
		{OperatingSystem: "darwin", Architecture: "arm64", CgoEnabled: true, Tags: []string{"second"}},
		{OperatingSystem: "linux", Architecture: "arm64"},
		{OperatingSystem: "windows", Architecture: "arm64"},
		{OperatingSystem: "freebsd", Architecture: "arm64"},
	}
	got := heaviestFirst(host, extras)
	if want := []int{3, 0, 1, 2, 4, 5}; !slices.Equal(got, want) {
		test.Errorf("the builds start in the order %v, want %v", got, want)
	}
	// However the order is reached, no second platform build starts before the host's first.
	platformsAhead := 0
	for _, index := range got {
		if extras[index].OperatingSystem == host.OperatingSystem {
			break
		}
		platformsAhead++
	}
	if platformsAhead != 1 {
		test.Errorf("%d platform builds start before the host's own, want the one measured", platformsAhead)
	}
	hostOnly := extras[:3]
	if got := heaviestFirst(host, hostOnly); !slices.Equal(got, []int{0, 1, 2}) {
		test.Errorf("builds all on the host's platform start in the order %v, want build order", got)
	}
	if got := heaviestFirst(host, nil); len(got) != 0 {
		test.Errorf("no builds start in the order %v, want none", got)
	}
}

// The width is decided by what the first build holds, the live heap measured as its load ends less the host's,
// never by whenever the collector last ran, so two decisions over one heap agree whatever state the collector is in:
// one right after a collection, one after a collection that counted 512 MiB the decision no longer holds, which a read
// of the last collection would count as the build's (#rmzehfg). A retained 256 MiB stands for the first build, and the
// limit is set so that holding, taken at its peak, leaves room for two builds, and the holding with the garbage
// counted for none beside the host's heap.
//
// Not parallel: it measures this process's live heap, which another test's run would move.
func TestTheBuildsWidthIsTheSameWhateverTheCollectorLastSaw(test *testing.T) {
	const garbageBytes = 512 << 20
	const buildBytes = 256 << 20
	hostLive := forcedLiveHeapBytes()
	// Room for two peaks of the build, 2.5 times its holding each, since the engine here has no findings store, and
	// half a holding more.
	limit := int64(hostLive + 5*buildBytes + buildBytes/2)
	engine := &pipeline{memory: &engineMemory{record: contract.Memory{LimitBytes: limit, CapBytes: limit,
		Source: contract.MemorySourceCap}}}
	build := make([]byte, buildBytes)
	for index := range build {
		build[index] = 1
	}

	runtime.GC()
	collectedWidth, _, collectedHolding := engine.measuredBuildsWidth(8, hostLive)

	garbage := make([]byte, garbageBytes)
	for index := range garbage {
		garbage[index] = 1
	}
	runtime.GC()
	runtime.KeepAlive(garbage)
	// garbage is unreachable from here, and no collection has run since the last one counted it.
	staleWidth, _, staleHolding := engine.measuredBuildsWidth(8, hostLive)
	runtime.KeepAlive(build)

	if collectedWidth != staleWidth {
		test.Errorf("the width is %d after a collection and %d after one that counted garbage, want one width",
			collectedWidth, staleWidth)
	}
	if collectedWidth != 2 {
		test.Errorf("the width is %d, want 2, the builds at their peaks the limit holds beside the host's heap",
			collectedWidth)
	}
	if difference := max(collectedHolding, staleHolding) - min(collectedHolding, staleHolding); difference > 64<<20 {
		test.Errorf("the decision was given holdings of %d and %d bytes, %d apart, want the same holding measured "+
			"both times", collectedHolding, staleHolding, difference)
	}
}

// Builds that restore nothing from the host's run run one at a time, whatever width is asked: each checks every
// package, and two of those side by side under the cap made a run slower (extraBuildsWidth). With the restore off, a
// run asked for three at once over three extra builds runs one at a time and says so.
func TestBuildsThatRestoreNothingRunOneAtATime(test *testing.T) {
	test.Parallel()

	engine, raw := runOverConfigured(test, threeExtraBuilds(test), exitFindings, func(engine *pipeline) {
		engine.keepsHostRestoreOff = true
		engine.buildsWidthForced = 3
	}, "--no-cache", "--other-builds")
	if len(engine.buildRuns) != 4 || engine.buildsAtOnce != 1 {
		test.Fatalf("with the restore off the run ran %d builds beside the host's, %d at once, want 3, one at a time",
			len(engine.buildRuns)-1, engine.buildsAtOnce)
	}
	if !strings.Contains(raw, `"buildsAtOnce":1`) {
		test.Errorf("the lint record does not say one build at a time:\n%s", raw)
	}
}

// withinBesideWait runs run, and fails test when it has not returned within besideWait, so a gate that holds a build's
// weight past its rules fails the test rather than hang it.
func withinBesideWait(test *testing.T, what string, run func()) {
	test.Helper()
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		run()
	}()
	select {
	case <-ended:
	case <-time.After(besideWait):
		test.Fatalf("%s had not ended after %s", what, besideWait)
	}
}

// awaitStep waits for ready to close, and fails test when besideWait passes first, which only a gate that never lets
// the awaited step come reaches. It is called from a build's own goroutine, so it reports with Errorf and returns.
func awaitStep(test *testing.T, ready <-chan struct{}, what string) {
	awaitStepUnlessBroken(test, ready, nil, what)
}

// awaitStepUnlessBroken is awaitStep that also returns when broken closes, which a test closes once it has failed for
// a reason the awaited step can never come after, so the test ends at once with that reason, not at besideWait.
func awaitStepUnlessBroken(test *testing.T, ready <-chan struct{}, broken <-chan struct{}, what string) {
	select {
	case <-ready:
	case <-broken:
	case <-time.After(besideWait):
		test.Errorf("%s never came within %s", what, besideWait)
	}
}

// extraBuildRecords is the lint record's builds in a stream as written, but the host's, failing test when it has none.
func extraBuildRecords(test *testing.T, stream string) []contract.BuildRecord {
	test.Helper()
	for line := range strings.Lines(stream) {
		var record contract.LintRecord
		if !strings.HasPrefix(line, `{"kind":"lint"`) || json.Unmarshal([]byte(line), &record) != nil {
			continue
		}
		if len(record.Builds) < 2 {
			break
		}
		return record.Builds[1:]
	}
	test.Fatalf("the stream holds no lint record with builds beside the host's:\n%s", stream)
	return nil
}

// checksHalf reports whether record's first load checked at least half of its packages from source, the share at or
// above which a build's lint takes the whole budget.
func checksHalf(record contract.BuildRecord) bool {
	return record.CheckedFromSource*lintBudget >= heavyLintShare*record.PackagesLoaded
}

// A build that checks most of its packages from source lints alone (#b2t9xmf): over nearFullWindows at width 3, the
// records say windows checked at least half of its graph from source and the other two less than half, and with each
// lint made to hold its weight until every build has come to the gate, no lint ever held weight beside windows'. With
// the gate granting every weight at once, all three would hold weight together.
func TestNoLintHoldsWeightBesideANearFullOne(test *testing.T) {
	test.Parallel()
	steps := newGateSteps()
	everyReady := make(chan struct{})
	var readyLock sync.Mutex
	ready := 0
	configure := func(engine *pipeline) {
		engine.buildsWidthForced = 3
		extras := len(engine.described.builds) - 1
		engine.lintSteps = func(extra project.Build, step lintStep, weight int) {
			steps.step(extra, step, weight)
			switch step {
			case lintStepReady:
				readyLock.Lock()
				defer readyLock.Unlock()
				ready++
				if ready == extras {
					close(everyReady)
				}
			case lintStepTaken:
				// Every build comes to the gate without weight, so this ends whatever the gate does, and a gate that
				// lets a lint beside a near-full one has them hold weight together here.
				awaitStep(test, everyReady, "every build at the gate")
			case lintStepQueued, lintStepGiving:
			}
		}
	}
	engine, raw := runOverConfigured(test, nearFullWindows(test, 0), exitFindings, configure, "--no-cache",
		"--other-builds")
	if engine.buildsAtOnce != 3 || len(engine.buildRuns) != 4 {
		test.Fatalf("the run ran %d builds beside the host's, %d at once, want 3 at once", len(engine.buildRuns)-1,
			engine.buildsAtOnce)
	}
	for _, record := range extraBuildRecords(test, raw) {
		windows := strings.HasPrefix(record.Target, "windows/")
		if record.PackagesLoaded == 0 || checksHalf(record) != windows {
			test.Errorf("%s checked %d of %d packages from source, want at least half for windows alone",
				record.Target, record.CheckedFromSource, record.PackagesLoaded)
		}
	}
	steps.lock.Lock()
	defer steps.lock.Unlock()
	if len(steps.beside) > 0 {
		test.Errorf("lints held weight beside a near-full one: %q", steps.beside)
	}
	if len(steps.taken) != 3 || steps.given != 3 || len(steps.holding) != 0 {
		test.Errorf("the builds took weight %d times and gave it back %d times, %v still holding, want three each",
			len(steps.taken), steps.given, steps.holding)
	}
}

// The gate admits in the order the builds come to it (#b2t9xmf): with one light build holding weight, windows, near
// full, comes and waits in line, and only then the other light build comes, which the budget would hold beside the
// first. It waits behind windows rather than pass it, so the builds take their weight light, windows, light, and the
// two that waited say so in their records. A gate that let a build the budget holds pass a waiting one would admit the
// second light build before windows.
func TestTheGateAdmitsInTheOrderTheBuildsCome(test *testing.T) {
	test.Parallel()
	steps := newGateSteps()
	var rolesLock sync.Mutex
	firstLight, secondLight := "", ""
	firstHolding := make(chan struct{})
	windowsInLine := make(chan struct{})
	secondAsked := make(chan struct{})
	var secondAskedOnce sync.Once
	// misweighed closes when a build comes with the wrong weight, windows light or another build near full: the order
	// the test plants can then never come, so every wait ends at once and the test fails with the weights it saw.
	misweighed := make(chan struct{})
	var misweighedOnce sync.Once
	role := func(name string) (bool, bool) {
		rolesLock.Lock()
		defer rolesLock.Unlock()
		return name == firstLight, name == secondLight
	}
	configure := func(engine *pipeline) {
		engine.buildsWidthForced = 3
		engine.lintSteps = func(extra project.Build, step lintStep, weight int) {
			steps.step(extra, step, weight)
			name := extra.String()
			heavy := weight >= lintBudget
			switch step {
			case lintStepReady:
				if heavy != (extra.OperatingSystem == "windows") {
					test.Errorf("%s came to the gate weighing %d of %d, want windows alone near full", name, weight,
						lintBudget)
					misweighedOnce.Do(func() { close(misweighed) })
					return
				}
				if heavy {
					awaitStepUnlessBroken(test, firstHolding, misweighed, "the first light build holding weight")
					return
				}
				rolesLock.Lock()
				first := firstLight == ""
				if first {
					firstLight = name
				} else {
					secondLight = name
				}
				rolesLock.Unlock()
				if !first {
					awaitStepUnlessBroken(test, windowsInLine, misweighed, "windows in line")
				}
			case lintStepQueued:
				if heavy && extra.OperatingSystem == "windows" {
					close(windowsInLine)
				}
				if _, second := role(name); second {
					secondAskedOnce.Do(func() { close(secondAsked) })
				}
			case lintStepTaken:
				first, second := role(name)
				if second {
					secondAskedOnce.Do(func() { close(secondAsked) })
				}
				// The first light build holds its weight until the second has asked for its own, in line or taken.
				if first {
					close(firstHolding)
					awaitStepUnlessBroken(test, secondAsked, misweighed, "the second light build asking for weight")
				}
			case lintStepGiving:
			}
		}
	}
	engine, raw := runOverConfigured(test, nearFullWindows(test, 0), exitFindings, configure, "--no-cache",
		"--other-builds")
	if engine.buildsAtOnce != 3 || len(engine.buildRuns) != 4 {
		test.Fatalf("the run ran %d builds beside the host's, %d at once, want 3 at once", len(engine.buildRuns)-1,
			engine.buildsAtOnce)
	}
	windows := slices.IndexFunc(engine.buildRuns, func(run buildRun) bool {
		return run.build.OperatingSystem == "windows"
	})
	if windows < 1 {
		test.Fatal("the run checked no windows build beside the host's")
	}
	windowsName := engine.buildRuns[windows].build.String()
	steps.lock.Lock()
	taken := slices.Clone(steps.taken)
	steps.lock.Unlock()
	want := []string{firstLight, windowsName, secondLight}
	if !slices.Equal(taken, want) {
		test.Errorf("the builds took their weight in the order %q, want %q", taken, want)
	}
	for _, record := range extraBuildRecords(test, raw) {
		wantWaited := record.Target != firstLight
		if record.LintWaited != wantWaited {
			test.Errorf("%s's record says its lint waited %t, want %t", record.Target, record.LintWaited, wantWaited)
		}
	}
}

// Many light builds and one near-full side by side share the run's one memory limit as they share the gate: over
// nearFullWindows with four more tagged builds at width 4 with the limit watched, no lint held weight beside windows',
// every load runs under the cap, no limit the run sets passes the cap plus lateMemoryAllowance, and every raise lands
// at exactly that, the one raise (#44n0vap, #b2t9xmf).
func TestManyLightBuildsAndANearFullOneNeverPassTheOneRaise(test *testing.T) {
	test.Parallel()
	steps := newGateSteps()
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
		engine.buildsWidthForced = 4
		engine.lintSteps = steps.step
	}
	engine, raw := runOverConfigured(test, nearFullWindows(test, 4), exitFindings, configure, "--no-cache",
		"--other-builds")
	if engine.buildsAtOnce != 4 || len(engine.buildRuns) != 8 {
		test.Fatalf("the run ran %d builds beside the host's, %d at once, want 7, 4 at once", len(engine.buildRuns)-1,
			engine.buildsAtOnce)
	}
	heavy := 0
	for _, record := range extraBuildRecords(test, raw) {
		if checksHalf(record) {
			heavy++
		}
	}
	if heavy != 1 {
		test.Errorf("%d builds checked at least half their packages from source, want windows alone", heavy)
	}
	steps.lock.Lock()
	if len(steps.beside) > 0 {
		test.Errorf("lints held weight beside a near-full one: %q", steps.beside)
	}
	steps.lock.Unlock()
	ceiling := engineMemoryCap + lateMemoryAllowance
	watch.lock.Lock()
	defer watch.lock.Unlock()
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
	raised := engine.loads.raisedTo()
	if len(raised) == 0 {
		test.Fatal("no load's raise landed, so the one raise went unread")
	}
	for _, limit := range raised {
		if limit != ceiling {
			test.Errorf("a raise landed at %d, want every raise at the cap plus one allowance, %d", limit, ceiling)
		}
	}
}

// One build at a time never waits at the gate: over nearFullWindows at width 1 no build's lint is in line, and every
// extra build's record says its lint did not wait.
func TestOneBuildAtATimeNeverWaitsAtTheGate(test *testing.T) {
	test.Parallel()
	steps := newGateSteps()
	engine, raw := runOverConfigured(test, nearFullWindows(test, 0), exitFindings, func(engine *pipeline) {
		engine.buildsWidthForced = 1
		engine.lintSteps = steps.step
	}, "--no-cache", "--other-builds")
	if engine.buildsAtOnce != 1 || len(engine.buildRuns) != 4 {
		test.Fatalf("the run ran %d builds beside the host's, %d at once, want 3, one at a time",
			len(engine.buildRuns)-1, engine.buildsAtOnce)
	}
	for _, record := range extraBuildRecords(test, raw) {
		if record.LintWaited || record.LintWaitMilliseconds != 0 {
			test.Errorf("%s's lint waited %t, %d ms, one build at a time", record.Target, record.LintWaited,
				record.LintWaitMilliseconds)
		}
	}
	steps.lock.Lock()
	defer steps.lock.Unlock()
	if len(steps.queued) > 0 || len(steps.taken) != 3 {
		test.Errorf("one build at a time, %v waited in line and %d took weight, want none and three", steps.queued,
			len(steps.taken))
	}
}

// A build's weight is its share of packages checked from source, rounded up, and the whole budget at or above one
// half; a load of no package weighs nothing.
func TestALintsWeightIsItsShareCheckedFromSource(test *testing.T) {
	test.Parallel()
	for _, each := range []struct {
		checked int
		loaded  int
		want    int
	}{
		{checked: 0, loaded: 12, want: 0},
		{checked: 1, loaded: 12, want: 84},
		{checked: 5, loaded: 12, want: 417},
		{checked: 6, loaded: 12, want: lintBudget},
		{checked: 5, loaded: 9, want: lintBudget},
		{checked: 12, loaded: 12, want: lintBudget},
		{checked: 0, loaded: 0, want: 0},
	} {
		if got := lintWeight(each.checked, each.loaded); got != each.want {
			test.Errorf("%d of %d checked from source weighs %d, want %d", each.checked, each.loaded, got, each.want)
		}
	}
}

// The sampler under `--timing` keeps the largest live heap it saw while the rules ran, not the last (#rmzehfg): 256 MiB
// held across two of its collections and let go before it stops is still in the peak it returns.
//
// Not parallel: it measures this process's live heap, which another test's run would move.
func TestTheLintPeakIsTheLargestHeapSampled(test *testing.T) {
	const heldBytes = 256 << 20
	stop := heldWhileLinting(time.Millisecond)
	held := make([]byte, heldBytes)
	for index := range held {
		held[index] = 1
	}
	forced := []metrics.Sample{{Name: forcedCyclesMetric}}
	metrics.Read(forced)
	since, counted := forcedCycles(forced)
	if !counted {
		test.Skip("this runtime counts no forced collections, so the test cannot tell when the sampler has sampled")
	}
	deadline := time.Now().Add(30 * time.Second)
	for cycles := since; cycles < since+2; cycles, _ = forcedCycles(forced) {
		if time.Now().After(deadline) {
			test.Fatal("the sampler forced no two collections in 30 s")
		}
		time.Sleep(time.Millisecond)
		metrics.Read(forced)
	}
	runtime.KeepAlive(held)
	held = nil
	peak := stop()
	if after := forcedLiveHeapBytes(); peak < heldBytes || peak <= after+heldBytes/2 {
		test.Errorf("the sampled peak is %d bytes, with %d live once it was let go, want at least the %d held, "+
			"well above the heap after", peak, after, heldBytes)
	}
}
