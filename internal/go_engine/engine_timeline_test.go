package go_engine

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
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

// buildTimeline is every stamp each extra build takes, under its name, in the order taken.
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

// timelineRun is the engine's stream over root with the registry's rules and its findings cache in root's own store,
// under `--no-fix` unless fixes, so nothing is written to the tree, and extra, its request changed by change when not
// nil, its extra builds run width at once, as a test forces it (buildsWidthForced), and its exit, which the stream
// agrees with, wantExit.
func timelineRun(
	test *testing.T,
	root string,
	width int,
	change func(request *invocation),
	wantExit int,
	extra ...string,
) []string {
	test.Helper()
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", root}, extra...)
	request, parseError := parseInvocation(arguments)
	if parseError != nil {
		test.Fatal(parseError)
	}
	if change != nil {
		change(&request)
	}
	_, standardOutput, standardError, exitCode := runOverRequest(test, request, rules.All(), func(engine *pipeline) {
		engine.buildsWidthForced = width
		// The width applies whether or not the builds restore from the host's run.
		engine.buildsWidthUnguarded = true
	})
	checkStream(test, standardOutput, exitCode)
	if exitCode != wantExit {
		test.Fatalf("the run exited %d, want %d:\n%s\n%s", exitCode, wantExit, standardOutput, standardError)
	}
	return strings.Split(strings.TrimSuffix(standardOutput, "\n"), "\n")
}

// otherBuildsRun is timelineRun over root, a copy of the other-builds fixture, under `--no-fix`, which finds one.
func otherBuildsRun(test *testing.T, root string, width int, change func(*invocation), extra ...string) []string {
	test.Helper()
	return timelineRun(test, root, width, change, exitFindings, append([]string{"--no-fix"}, extra...)...)
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

// timelineOf is the timeline record of lines, which must be the last before the summary.
func timelineOf(test *testing.T, lines []string) contract.TimelineRecord {
	test.Helper()
	var record contract.TimelineRecord
	index := recordOfKind(test, lines, contract.KindTimeline, &record)
	if index < 0 || index != len(lines)-2 {
		test.Fatalf("the timeline record is line %d of %d, want the last before the summary", index+1, len(lines))
	}
	return record
}

// timelineIdentity is what makes a stamp one of its run's: its build, its round and its label.
type timelineIdentity struct {
	build string
	round int
	label string
}

// requireUniqueStamps fails the test for each stamp of record that shares its build, round and label with one before
// it, and for a wall that goes back.
func requireUniqueStamps(test *testing.T, record contract.TimelineRecord) {
	test.Helper()
	seen := map[timelineIdentity]bool{}
	for position, stamp := range record.Stamps {
		identity := timelineIdentity{build: stamp.Build, round: stamp.Round, label: stamp.Label}
		if seen[identity] {
			test.Errorf("stamp %+v is a second one of build %q, round %d, label %s", stamp, stamp.Build,
				stamp.Round, stamp.Label)
		}
		seen[identity] = true
		if position > 0 && stamp.WallMicroseconds < record.Stamps[position-1].WallMicroseconds {
			test.Errorf("stamp %+v's wall is before the stamp's before it, %+v", stamp, record.Stamps[position-1])
		}
	}
}

// A cold run of every build under `--timeline`, its builds one at a time and two at once, writes one timeline record,
// the last before the summary, whose stamps are the host's in their order with each extra build's in theirs, under its
// name, between the handover's end and the builds' end, however the two builds' interleave, no stamp of any other
// label, every build the lint record names stamped, no two stamps alike, and a wall that never goes back (#3v2zp7g).
func TestATimelineMapsEveryPhaseAndBuildInOrder(test *testing.T) {
	test.Parallel()
	for _, width := range []int{1, 2} {
		test.Run(fmt.Sprintf("%d at once", width), func(test *testing.T) {
			test.Parallel()
			lines := otherBuildsRun(test, copyFixture(test, filepath.Join("testdata", "other-builds")), width, nil,
				"--timeline")
			record := timelineOf(test, lines)
			if record.Timed || record.Unavailable != "" {
				test.Errorf("an untimed run's timeline is timed %v, unavailable %q", record.Timed, record.Unavailable)
			}
			requireUniqueStamps(test, record)

			byBuild := map[string][]string{}
			var host []string
			handoverEnd, buildsEnd := -1, -1
			for position, stamp := range record.Stamps {
				if stamp.UserMicroseconds <= 0 || stamp.SystemMicroseconds < 0 || stamp.Round != 0 {
					test.Errorf("stamp %+v holds no CPU or is of a round past the first", stamp)
				}
				if stamp.Build == "" {
					host = append(host, stamp.Label)
					switch stamp.Label {
					case "handover.end":
						handoverEnd = position
					case "builds.end":
						buildsEnd = position
					}
					continue
				}
				byBuild[stamp.Build] = append(byBuild[stamp.Build], stamp.Label)
				if handoverEnd < 0 || buildsEnd >= 0 {
					test.Errorf("build %s stamped %s outside the handover's end and the builds' end", stamp.Build,
						stamp.Label)
				}
			}
			if !slices.Equal(host, hostTimeline) {
				test.Errorf("the run stamped, outside its builds,\n  %q\nwant\n  %q", host, hostTimeline)
			}

			var lint contract.LintRecord
			recordOfKind(test, lines, contract.KindLint, &lint)
			if lint.BuildsAtOnce != width {
				test.Errorf("the builds ran %d at once, want %d", lint.BuildsAtOnce, width)
			}
			var extraTargets []string
			for _, build := range lint.Builds {
				if !build.Host {
					extraTargets = append(extraTargets, build.Target)
				}
			}
			if len(extraTargets) < 2 {
				test.Fatalf("the lint record names extra builds %q, and the fixture has two", extraTargets)
			}
			if targets := slices.Sorted(maps.Keys(byBuild)); !slices.Equal(targets,
				slices.Sorted(slices.Values(extraTargets))) {
				test.Errorf("the timeline stamped builds %q, and the lint record names %q", targets, extraTargets)
			}
			for target, steps := range byBuild {
				if !slices.Equal(steps, buildTimeline) {
					test.Errorf("build %s stamped\n  %q\nwant\n  %q", target, steps, buildTimeline)
				}
			}
		})
	}
}

// hostLoadTimeline is every stamp of one host load, in the order taken: its listing, its restore from the findings
// cache, its first parse and its end.
var hostLoadTimeline = []string{
	"host.listing.end",
	"host.restore.begin",
	"host.restore.end",
	"host.parse.first",
	"host.load.end",
}

// hostCheckTimeline is every stamp of one host lint pass with a findings cache, in the order taken.
var hostCheckTimeline = []string{
	"host.check.analyze.begin",
	"host.check.analyze.end",
	"host.check.save.begin",
	"host.check.save.end",
	"host.lint.check.end",
}

// A load or a check that runs again stamps in its own round: a fixing run whose long-line wrap wrote loads the
// workspace a second time, whose steps carry round 1, each load's steps whole and in order, and its one lint pass, over
// the wrap's load, carries that load's round, 1, so a reader joins the check to its load. No two stamps are alike. A
// run that loads nothing, `--format-only`, stamps no load at all.
func TestATimelineTellsALoadThatRunsAgainByItsRound(test *testing.T) {
	test.Parallel()
	record := timelineOf(test, timelineRun(test, copyFixture(test, filepath.Join("testdata", "long-lines")), 0, nil,
		exitFindings, "--timeline"))
	requireUniqueStamps(test, record)
	loads := map[int][]string{}
	checks := map[int][]string{}
	for _, stamp := range record.Stamps {
		switch {
		case slices.Contains(hostLoadTimeline, stamp.Label):
			loads[stamp.Round] = append(loads[stamp.Round], stamp.Label)
		case slices.Contains(hostCheckTimeline, stamp.Label):
			checks[stamp.Round] = append(checks[stamp.Round], stamp.Label)
		}
	}
	for round := range 2 {
		if !slices.Equal(loads[round], hostLoadTimeline) {
			test.Errorf("the load of round %d stamped\n  %q\nwant\n  %q", round, loads[round], hostLoadTimeline)
		}
	}
	if len(loads) != 2 {
		test.Errorf("the run stamped loads of rounds %v, want the first and the wrap's",
			slices.Sorted(maps.Keys(loads)))
	}
	if len(checks) != 1 || !slices.Equal(checks[1], hostCheckTimeline) {
		test.Errorf(
			"the run stamped checks %v, want one, in round 1, the round of the wrap's load it runs over",
			checks,
		)
	}

	formatOnly := timelineOf(test, timelineRun(test, copyFixture(test, filepath.Join("testdata", "unformatted")), 0,
		nil, exitFindings, "--timeline", "--format-only", "--no-fix"))
	for _, stamp := range formatOnly.Stamps {
		if strings.HasPrefix(stamp.Label, "host.") {
			test.Errorf("a run that loads nothing stamped %+v", stamp)
		}
	}
}

// buildReloadTimeline is every stamp a build's reload for a question takes, in its round, in the order taken: its
// load's listing, restore, first parse and end, then its check over that load.
var buildReloadTimeline = []string{
	"listing.end",
	"restore.begin",
	"restore.end",
	"parse.first",
	"load.end",
	"check.analyze.begin",
	"check.analyze.end",
	"check.save.begin",
	"check.save.end",
}

// A build loaded again stamps the reload in its own round: over the fixture whose windows build declines two packages
// at its check and loads once more (TestAReloadsChecksFromSourceReachTheRecord), windows stamps the reload's load,
// its end among them, and the check over it in round 1, every other build stamps nothing past round 0, and no two
// stamps are alike.
func TestATimelineTellsABuildsReloadByItsRound(test *testing.T) {
	test.Parallel()
	record := timelineOf(test, timelineRun(test, copyFixture(test, filepath.Join("testdata", "restore-from-host")), 0,
		nil, exitFindings, "--no-cache", "--timeline"))
	requireUniqueStamps(test, record)
	reloads := map[string][]string{}
	for _, stamp := range record.Stamps {
		if stamp.Build != "" && stamp.Round > 0 {
			if stamp.Round != 1 {
				test.Errorf("stamp %+v is of round %d, and the fixture reloads once", stamp, stamp.Round)
			}
			reloads[stamp.Build] = append(reloads[stamp.Build], stamp.Label)
		}
	}
	if len(reloads) != 1 {
		test.Fatalf("builds %v stamped a reload, want windows alone", slices.Sorted(maps.Keys(reloads)))
	}
	for build, steps := range reloads {
		if !strings.HasPrefix(build, "windows/") || !slices.Equal(steps, buildReloadTimeline) {
			test.Errorf("build %s stamped its reload\n  %q\nwant windows's\n  %q", build, steps, buildReloadTimeline)
		}
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

// A run without `--timeline` makes no recorder: it reads the clock its request hands a timeline not once and writes no
// timeline record. The same run under the switch reads it, so a count of 0 is the switch's and not a clock the run
// never reached. It counts the reads of that clock alone: a bare read of the system's clock at a stamp site is no
// read of it.
func TestARunWithoutTheTimelineReadsNoClock(test *testing.T) {
	test.Parallel()
	counting := &countingClock{}
	counted := func(request *invocation) { request.timelineClock = counting.clock() }
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	lines := otherBuildsRun(test, root, 1, counted)
	if reads := counting.reads.Load(); reads != 0 {
		test.Errorf("a run without --timeline read the timeline's clock %d times, want none", reads)
	}
	var record contract.TimelineRecord
	if index := recordOfKind(test, lines, contract.KindTimeline, &record); index >= 0 {
		test.Errorf("a run without --timeline wrote a timeline record on line %d", index+1)
	}

	otherBuildsRun(test, root, 1, counted, "--timeline")
	if counting.reads.Load() == 0 {
		test.Error("a run under --timeline read no clock its request handed it, so a count of 0 proves nothing")
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

// buildReads is what an extra build read of the host's run and of its stores, as its record in the lint record says.
type buildReads struct {
	target           string
	restoredFromHost int
	untouched        int
	writtenThrough   int
	packs            contract.BuildPacks
}

// buildReadsOf is each extra build's reads in lint, in the record's order.
func buildReadsOf(lint contract.LintRecord) []buildReads {
	var reads []buildReads
	for _, build := range lint.Builds {
		if build.Host {
			continue
		}
		read := buildReads{target: build.Target, restoredFromHost: build.RestoredFromHost,
			untouched: build.Untouched, writtenThrough: build.WrittenThrough}
		if build.Packs != nil {
			read.packs = *build.Packs
		}
		reads = append(reads, read)
	}
	return reads
}

// `--timeline` changes nothing about the run: over one tree, each from an empty store and an empty system cache, a run
// with it and one without, their builds forced two at once, write the same findings, the same files into the findings
// cache's store, the same cache counts, the system cache's among them, each build restoring, leaving untouched and
// writing through as many packages and reading its packs alike, and run two builds at once (#3v2zp7g), where
// `--timing` turns the cache off and runs the builds one at a time.
func TestTheTimelineChangesNothingAboutTheRun(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	mapped := otherBuildsRun(test, root, 2, nil, "--timeline")
	mappedStore := storeListing(test, root)
	if removeError := os.RemoveAll(cache_directory.Of(root)); removeError != nil {
		test.Fatal(removeError)
	}
	plain := otherBuildsRun(test, root, 2, nil)
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
	plainLint := lintRecordOf(test, strings.Join(plain, "\n"))
	mappedLint := lintRecordOf(test, strings.Join(mapped, "\n"))
	for name, lint := range map[string]contract.LintRecord{"without --timeline": plainLint, "with it": mappedLint} {
		if lint.BuildsAtOnce != 2 {
			test.Errorf("the builds ran %d at once %s, want the 2 forced", lint.BuildsAtOnce, name)
		}
	}
	if !reflect.DeepEqual(plainLint.Cache, mappedLint.Cache) {
		test.Errorf("the cache counts are\n  %+v\nwithout --timeline, and\n  %+v\nwith it", plainLint.Cache,
			mappedLint.Cache)
	}
	if plainReads, mappedReads := buildReadsOf(plainLint), buildReadsOf(mappedLint); len(plainReads) == 0 ||
		!reflect.DeepEqual(plainReads, mappedReads) {
		test.Errorf("the builds read\n  %+v\nwithout --timeline, and\n  %+v\nwith it", plainReads, mappedReads)
	}
}
