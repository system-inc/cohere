package go_engine

import (
	"encoding/json"
	"fmt"
	"io"
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
	"github.com/system-inc/cohere/internal/go_engine/project"
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

// buildReads is what an extra build read of the host's run and of its stores, as its record in the lint record says,
// but the shard stamps its packs read, left at 0 here and counted over the run (shardReadsOf): a shard's stamp is read
// once a run, by the build that asks first (cache.ShardStamps.Of), and counted by that build alone, so how builds
// side by side split them is the order they asked in, and only their sum is the run's (#nyrys1z).
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
			read.packs.ShardReads = 0
		}
		reads = append(reads, read)
	}
	return reads
}

// shardReadsOf is the shard stamps each extra build of lint read, by its target, and their sum, the run's.
func shardReadsOf(lint contract.LintRecord) (map[string]int, int) {
	byBuild, sum := map[string]int{}, 0
	for _, build := range lint.Builds {
		if build.Host || build.Packs == nil {
			continue
		}
		byBuild[build.Target] = build.Packs.ShardReads
		sum += build.Packs.ShardReads
	}
	return byBuild, sum
}

// comparedRun is one of two runs over one tree that a test compares (requireTheSameRun): what a message calls it, its
// stream, and the files it left in the findings cache's store (storeListing).
type comparedRun struct {
	name   string
	stream string
	store  []string
}

// requireTheSameRun fails the test for each thing a run fixes that left and right, two runs over one tree with their
// builds forced two at once, do not share: the findings, the files in the findings cache's store, the cache counts,
// the system cache's and the builds' shard reads summed among them, each build restoring, leaving untouched and
// writing through as many packages and reading its packs alike but for its share of the shard reads (buildReads), and
// the two builds at once.
func requireTheSameRun(test *testing.T, left comparedRun, right comparedRun) {
	test.Helper()
	if len(left.store) == 0 {
		test.Fatalf("the run %s wrote nothing to its store, so there is nothing to compare", left.name)
	}
	if !slices.Equal(left.store, right.store) {
		test.Errorf("the store holds\n  %q\n%s, and\n  %q\n%s", left.store, left.name, right.store, right.name)
	}
	leftFindings, rightFindings := findingLines(left.stream), findingLines(right.stream)
	if len(leftFindings) == 0 || !slices.Equal(leftFindings, rightFindings) {
		test.Errorf("the findings are\n%s\n%s, and\n%s\n%s", strings.Join(leftFindings, ""), left.name,
			strings.Join(rightFindings, ""), right.name)
	}
	leftLint, rightLint := lintRecordOf(test, left.stream), lintRecordOf(test, right.stream)
	for name, lint := range map[string]contract.LintRecord{left.name: leftLint, right.name: rightLint} {
		if lint.BuildsAtOnce != 2 {
			test.Errorf("the builds ran %d at once %s, want the 2 forced", lint.BuildsAtOnce, name)
		}
	}
	if !reflect.DeepEqual(leftLint.Cache, rightLint.Cache) {
		test.Errorf("the cache counts are\n  %+v\n%s, and\n  %+v\n%s", leftLint.Cache, left.name, rightLint.Cache,
			right.name)
	}
	if leftReads, rightReads := buildReadsOf(leftLint), buildReadsOf(rightLint); len(leftReads) == 0 ||
		!reflect.DeepEqual(leftReads, rightReads) {
		test.Errorf("the builds read\n  %+v\n%s, and\n  %+v\n%s", leftReads, left.name, rightReads, right.name)
	}
	leftSplit, leftShardReads := shardReadsOf(leftLint)
	rightSplit, rightShardReads := shardReadsOf(rightLint)
	if leftShardReads == 0 || leftShardReads != rightShardReads {
		test.Errorf("the builds read %d shard stamps %s, split %v, and %d %s, split %v, want some and as many",
			leftShardReads, left.name, leftSplit, rightShardReads, right.name, rightSplit)
	}
}

// `--timeline` changes nothing about the run: over one tree, each from an empty store and an empty system cache, a run
// with it and one without, their builds forced two at once, write the same findings, the same files into the findings
// cache's store, the same cache counts, the system cache's among them, each build restoring, leaving untouched and
// writing through as many packages and reading its packs alike, and run two builds at once (#3v2zp7g), where
// `--timing` turns the cache off and runs the builds one at a time. The shard stamps the builds read are compared as
// their sum, never build by build: which of two builds side by side reads a shard's stamp is whichever asks first
// (TestTheBuildsOrderMovesOnlyTheSplitOfTheirShardReads), which no switch decides (#nyrys1z).
func TestTheTimelineChangesNothingAboutTheRun(test *testing.T) {
	test.Parallel()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	mapped := comparedRun{name: "with it", stream: strings.Join(otherBuildsRun(test, root, 2, nil, "--timeline"), "\n"),
		store: storeListing(test, root)}
	if removeError := os.RemoveAll(cache_directory.Of(root)); removeError != nil {
		test.Fatal(removeError)
	}
	plain := comparedRun{name: "without --timeline", stream: strings.Join(otherBuildsRun(test, root, 2, nil), "\n"),
		store: storeListing(test, root)}
	requireTheSameRun(test, plain, mapped)
}

// buildFirst is a pipeline's configuration that forces its extra builds two at once and has the one first names end
// before any other starts (extraBuildStarts, extraBuildEnds), so that build asks first for everything the run's builds
// read once between them, each shard's stamp among it. A build held past besideWait fails the test and goes on, so a
// first build that never ended reads as a failure rather than a run that never ends.
func buildFirst(test *testing.T, first func(build project.Build) bool) func(*pipeline) {
	ended := make(chan struct{})
	return func(engine *pipeline) {
		engine.buildsWidthForced = 2
		// The width applies whether or not the builds restore from the host's run.
		engine.buildsWidthUnguarded = true
		engine.extraBuildStarts = func(extra project.Build) {
			if first(extra) {
				return
			}
			select {
			case <-ended:
			case <-time.After(besideWait):
				test.Errorf("the build %s was held for a first build that never ended", extra)
			}
		}
		engine.extraBuildEnds = func(extra project.Build, _ io.Writer) error {
			if first(extra) {
				close(ended)
			}
			return nil
		}
	}
}

// sharedShardPackages is how many packages a test adds to its copy of the other-builds fixture so that its two extra
// builds are sure to share a shard (withSharedShards). A build asks its store for a key of each package it loads, a
// key's shard is its first byte (cache.ShardOf), and a key holds the path of each of its package's files
// (cache.Keys), so which shards two builds share varies with where the copy lies: over the fixture's three packages
// most copies share none, and the builds have no stamp to race for. With 150 more, each build's keys fall in about
// 115 of the store's 256 shards, and the odds that two such builds share none are under 1 in 10^30.
const sharedShardPackages = 150

// withSharedShards is a copy of the other-builds fixture with sharedShardPackages more packages, each one file every
// build compiles alike.
func withSharedShards(test *testing.T) string {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", "other-builds"))
	files := map[string]string{}
	for index := range sharedShardPackages {
		files[fmt.Sprintf("internal/keyed%d/keyed.go", index)] = fmt.Sprintf("// Package keyed%d is one more package "+
			"every build keys.\npackage keyed%d\n\n// Value is the package's value.\nconst Value = %d\n", index, index,
			index)
	}
	writeFixtureFiles(test, root, files)
	return root
}

// The order two builds side by side ask in moves how they split the shard stamps they read, and nothing else of the
// run (#nyrys1z): over one tree whose builds share shards (withSharedShards), each from an empty store and an empty
// system cache, a run whose tagged build ends before its windows build starts and one the other way round
// (buildFirst), both forced two at once, are the same run by everything TestTheTimelineChangesNothingAboutTheRun
// compares (requireTheSameRun), the shard reads summed among it, and split those reads otherwise: each build reads
// more of them asking first than held, when the build before it has read the stamps of the shards the two share.
func TestTheBuildsOrderMovesOnlyTheSplitOfTheirShardReads(test *testing.T) {
	test.Parallel()
	root := withSharedShards(test)
	tagged := func(build project.Build) bool { return slices.Contains(build.Tags, "cohere_extra") }
	ordered := func(name string, first func(build project.Build) bool) comparedRun {
		test.Helper()
		_, stream := runOverConfigured(test, root, exitFindings, buildFirst(test, first), "--no-fix")
		run := comparedRun{name: name, stream: stream, store: storeListing(test, root)}
		if removeError := os.RemoveAll(cache_directory.Of(root)); removeError != nil {
			test.Fatal(removeError)
		}
		return run
	}
	taggedFirst := ordered("with the tagged build first", tagged)
	windowsFirst := ordered("with the windows build first", func(build project.Build) bool { return !tagged(build) })
	requireTheSameRun(test, taggedFirst, windowsFirst)

	taggedFirstSplit, _ := shardReadsOf(lintRecordOf(test, taggedFirst.stream))
	windowsFirstSplit, _ := shardReadsOf(lintRecordOf(test, windowsFirst.stream))
	if len(taggedFirstSplit) != 2 || len(windowsFirstSplit) != 2 {
		test.Fatalf("the runs' extra builds read %v and %v, and the fixture has two", taggedFirstSplit,
			windowsFirstSplit)
	}
	for target, askingFirst := range taggedFirstSplit {
		held := windowsFirstSplit[target]
		if !strings.Contains(target, "cohere_extra") {
			askingFirst, held = held, askingFirst
		}
		if askingFirst <= held {
			test.Errorf("the build %s read %d shard stamps asking first and %d held until the other build ended, want "+
				"more asking first: the builds split them %v with the tagged build first and %v with the windows "+
				"build first", target, askingFirst, held, taggedFirstSplit, windowsFirstSplit)
		}
	}
}
