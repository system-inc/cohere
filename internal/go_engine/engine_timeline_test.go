package go_engine

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
	"github.com/system-inc/cohere/internal/go_engine/timeline"
)

// timelineBuildPrefix is how a build's stamps begin, before its target's name, and timelineBuildShape what the shape
// of a build's label puts in place of that name.
const (
	timelineBuildPrefix = "build "
	timelineBuildShape  = "build <target>."
)

// hostTimeline is every stamp a cold run of every build over the other-builds fixture takes outside its extra builds,
// in the order taken: the engine's start, the host's load, its check and its lint, the handover to the builds, and,
// after every build, the merge and the run's end.
var hostTimeline = []string{
	"engine.main.begin",
	"host.listing.end",
	"host.restore.begin",
	"host.restore.end",
	"host.parse.first",
	"host.load.end",
	"host.check.analyze.begin",
	"host.check.analyze.end",
	"host.check.save.begin",
	"host.check.save.end",
	"host.lint.check.end",
	"handover.begin",
	"host.readwalks.end",
	"handover.end",
	"builds.end",
	"merge.end",
	"run.end",
}

// buildTimeline is every stamp each extra build takes, after its target's name, in the order taken.
var buildTimeline = []string{
	"start",
	"listing.end",
	"restore.begin",
	"restore.end",
	"parse.first",
	"load.end",
	"gate.taken",
	"check.analyze.begin",
	"check.analyze.end",
	"check.save.begin",
	"check.save.end",
	"end",
}

// timelineStream is the engine's stream over root with the registry's rules and its findings cache in root's own
// store, under `--no-fix`, so nothing is written to the tree, and extra, and the exit it made, which the stream agrees
// with.
func timelineStream(test *testing.T, root string, extra ...string) []string {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root, "--no-fix"}, extra...)
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := runIsolated(test, arguments, &standardOutput, &standardError, rules.All(), nil)
	checkStream(test, standardOutput.String(), exitCode)
	if exitCode != exitFindings {
		test.Fatalf("the run exited %d, want %d:\n%s\n%s", exitCode, exitFindings, standardOutput.String(),
			standardError.String())
	}
	return strings.Split(strings.TrimSuffix(standardOutput.String(), "\n"), "\n")
}

// recordOfKind is the one record of kind in lines, decoded into record, with its index, -1 when there is none, and a
// failed test when there are two.
func recordOfKind[Record contract.Record](test *testing.T, lines []string, kind contract.Kind, record *Record) int {
	test.Helper()
	found := -1
	for index, line := range lines {
		if !strings.HasPrefix(line, `{"kind":"`+string(kind)+`"`) {
			continue
		}
		if found >= 0 {
			test.Fatalf("two %s records, lines %d and %d", kind, found+1, index+1)
		}
		found = index
		if decodeError := json.Unmarshal([]byte(line), record); decodeError != nil {
			test.Fatalf("decoding the %s record %q: %v", kind, line, decodeError)
		}
	}
	return found
}

// A cold run of every build under `--timeline` writes one timeline record, the last before the summary, whose stamps
// are the host's in their order with each extra build's in theirs between the handover's end and the builds' end, no
// stamp of any other shape, every build the lint record names stamped, and a wall that never goes back (#3v2zp7g).
func TestATimelineMapsEveryPhaseAndBuildInOrder(test *testing.T) {
	test.Parallel()
	lines := timelineStream(test, copyFixture(test, filepath.Join("testdata", "other-builds")), "--timeline")
	var record contract.TimelineRecord
	index := recordOfKind(test, lines, contract.KindTimeline, &record)
	if index < 0 || index != len(lines)-2 {
		test.Fatalf("the timeline record is line %d of %d, want the last before the summary", index+1, len(lines))
	}
	if record.Timed || record.Unavailable != "" {
		test.Errorf("an untimed run's timeline is timed %v, unavailable %q", record.Timed, record.Unavailable)
	}

	var shapes []string
	byBuild := map[string][]string{}
	var host []string
	handoverEnd, buildsEnd := -1, -1
	for position, stamp := range record.Stamps {
		if position > 0 && stamp.WallMicroseconds < record.Stamps[position-1].WallMicroseconds {
			test.Errorf("stamp %s's wall %dµs is before %s's, the stamp before it, %dµs", stamp.Label,
				stamp.WallMicroseconds, record.Stamps[position-1].Label, record.Stamps[position-1].WallMicroseconds)
		}
		if stamp.UserMicroseconds <= 0 || stamp.SystemMicroseconds < 0 {
			test.Errorf("stamp %s holds no CPU: %+v", stamp.Label, stamp)
		}
		named, isBuild := strings.CutPrefix(stamp.Label, timelineBuildPrefix)
		if !isBuild {
			host = append(host, stamp.Label)
			shapes = append(shapes, stamp.Label)
			switch stamp.Label {
			case "handover.end":
				handoverEnd = position
			case "builds.end":
				buildsEnd = position
			}
			continue
		}
		// A target's name holds no dot of its own, so the build's is the first.
		target, step, _ := strings.Cut(named, ".")
		byBuild[target] = append(byBuild[target], step)
		shapes = append(shapes, timelineBuildShape+step)
		if handoverEnd < 0 || buildsEnd >= 0 {
			test.Errorf("build %s stamped %s outside the handover's end and the builds' end", target, step)
		}
	}
	if !slices.Equal(host, hostTimeline) {
		test.Errorf("the run stamped, outside its builds,\n  %q\nwant\n  %q", host, hostTimeline)
	}

	var lint contract.LintRecord
	recordOfKind(test, lines, contract.KindLint, &lint)
	var extraTargets []string
	for _, build := range lint.Builds {
		if !build.Host {
			extraTargets = append(extraTargets, build.Target)
		}
	}
	if len(extraTargets) < 2 {
		test.Fatalf("the lint record names extra builds %q, and the fixture has two", extraTargets)
	}
	if targets := slices.Sorted(func(yield func(string) bool) {
		for target := range byBuild {
			if !yield(target) {
				return
			}
		}
	}); !slices.Equal(targets, slices.Sorted(slices.Values(extraTargets))) {
		test.Errorf("the timeline stamped builds %q, and the lint record names %q", targets, extraTargets)
	}
	for target, steps := range byBuild {
		if !slices.Equal(steps, buildTimeline) {
			test.Errorf("build %s stamped\n  %q\nwant\n  %q", target, steps, buildTimeline)
		}
	}

	// Every shape a stamp may take, so a stamp renamed, added or dropped anywhere fails here by its name.
	want := slices.Clone(hostTimeline)
	for _, step := range buildTimeline {
		want = append(want, timelineBuildShape+step)
	}
	slices.Sort(want)
	if got := slices.Compact(slices.Sorted(slices.Values(shapes))); !slices.Equal(got, want) {
		test.Errorf("the timeline's label shapes are\n  %q\nwant\n  %q", got, want)
	}
}

// countingClock is a timeline clock that counts every read of either of its clocks, and reads the system's.
type countingClock struct {
	reads atomic.Int64
}

// clock is the counted clock.
func (counting *countingClock) clock() timeline.Clock {
	system := timeline.SystemClock()
	return timeline.Clock{
		Now: func() time.Time {
			counting.reads.Add(1)
			return system.Now()
		},
		ProcessTimes: func() (time.Duration, time.Duration, bool) {
			counting.reads.Add(1)
			return system.ProcessTimes()
		},
	}
}

// A run without `--timeline` makes no recorder: it reads the timeline's clock not once and writes no timeline record,
// so the switch costs a run that did not ask nothing. The same run under the switch reads it, so a count of 0 is the
// switch's and not a clock the run never reached.
//
// Not parallel: it replaces the package-level timelineClock, which every run of this package reads as it starts.
func TestARunWithoutTheTimelineReadsNoClock(test *testing.T) {
	saved := timelineClock
	defer func() { timelineClock = saved }()
	counting := &countingClock{}
	timelineClock = counting.clock()

	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	lines := timelineStream(test, root)
	if reads := counting.reads.Load(); reads != 0 {
		test.Errorf("a run without --timeline read the timeline's clock %d times, want none", reads)
	}
	var record contract.TimelineRecord
	if index := recordOfKind(test, lines, contract.KindTimeline, &record); index >= 0 {
		test.Errorf("a run without --timeline wrote a timeline record on line %d", index+1)
	}

	timelineStream(test, root, "--timeline")
	if counting.reads.Load() == 0 {
		test.Error("a run under --timeline read no clock through timelineClock, so a count of 0 proves nothing")
	}
}

// storeListing is every file's path below the findings cache's store of root, relative to it, sorted, none when the
// run made no store.
func storeListing(test *testing.T, root string) []string {
	test.Helper()
	store := cache_directory.Of(root)
	var listing []string
	if _, statError := os.Stat(store); os.IsNotExist(statError) {
		return nil
	}
	walkError := filepath.WalkDir(store, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || entry.IsDir() {
			return pathError
		}
		relative, relativeError := filepath.Rel(store, path)
		listing = append(listing, relative)
		return relativeError
	})
	if walkError != nil {
		test.Fatalf("listing %s: %v", store, walkError)
	}
	slices.Sort(listing)
	return listing
}

// lintBuildsAtOnce is the lint record's buildsAtOnce in lines.
func lintBuildsAtOnce(test *testing.T, lines []string) int {
	test.Helper()
	var lint contract.LintRecord
	if recordOfKind(test, lines, contract.KindLint, &lint) < 0 {
		test.Fatal("the run wrote no lint record")
	}
	return lint.BuildsAtOnce
}

// `--timeline` changes nothing about the run: over one tree, each from an empty store, a run with it and one without
// write the same findings, the same files into the findings cache's store, and run the same builds at once (#3v2zp7g),
// where `--timing` turns the cache off and runs the builds one at a time.
func TestTheTimelineChangesNothingAboutTheRun(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	mapped := timelineStream(test, root, "--timeline")
	mappedStore := storeListing(test, root)
	if removeError := os.RemoveAll(cache_directory.Of(root)); removeError != nil {
		test.Fatal(removeError)
	}
	plain := timelineStream(test, root)
	plainStore := storeListing(test, root)
	if len(plainStore) == 0 {
		test.Fatal("the run without --timeline wrote nothing to its store, so there is nothing to compare")
	}
	if !slices.Equal(plainStore, mappedStore) {
		test.Errorf("the store holds\n  %q\nwithout --timeline, and\n  %q\nwith it", plainStore, mappedStore)
	}
	plainFindings := findingLines(strings.Join(plain, "\n"))
	mappedFindings := findingLines(strings.Join(mapped, "\n"))
	if len(plainFindings) == 0 || !slices.Equal(plainFindings, mappedFindings) {
		test.Errorf("the findings are\n%s\nwithout --timeline, and\n%s\nwith it", strings.Join(plainFindings, ""),
			strings.Join(mappedFindings, ""))
	}
	if plainWidth, mappedWidth := lintBuildsAtOnce(test, plain), lintBuildsAtOnce(test, mapped); plainWidth == 0 ||
		plainWidth != mappedWidth {
		test.Errorf("the builds ran %d at once without --timeline and %d with it", plainWidth, mappedWidth)
	}
}
