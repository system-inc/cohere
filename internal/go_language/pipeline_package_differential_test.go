package go_language

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_language/check"
	"github.com/system-inc/cohere/internal/go_language/project"
)

// The package differential (#wcwx8vf), the soundness contract's machine check pointed at what a build skips (#9paaen9,
// #mtrjfyn): every package an extra build restores unchecked is one TypesHold against the build's types would hold.
// A build restores a package unchecked two ways, which skippedOf names: the walk's untouched (check's untouchedOf),
// judged by nothing, and a visited package its late decision lets through. Under the test's hooks
// (pipeline.skippedAtLoad and judgedSkipped) each load tells of each package it restores so, each of those is judged
// by TypesHold beside the run, apart from the decision (check.CacheSession's JudgeSkipped), and the differential fails
// naming every one the judgment declines, but one the check declined for its facts or answers, which the build checked
// from source after all. Each load
// tells of itself too (pipeline.extraBuildLoads), so a build's loads are the test's own count, never one read from
// the run's records (#tfvrsf3).

// skippedOf is the seam: the packages run, an extra build's, restored unchecked over its last load, untouched or let
// through, by ID, as the run records them, each of which some load must have told the hook of. A later stage that
// skips a package another way adds its own skip here, and tells the hook of it.
func skippedOf(run buildRun) []string {
	if run.result == nil {
		return nil
	}
	return slices.Concat(run.result.FromHost.Untouched, run.result.FromHost.Restored)
}

// declinedAtCheck reports whether run, an extra build's, declined the package of identifier at check, for its facts or
// answers, so it checked it from source after all: such a package was let through at its load and asked at check, not
// restored unchecked.
func declinedAtCheck(run buildRun, identifier string) bool {
	if run.result == nil {
		return false
	}
	why := run.result.FromHost.Declined[identifier]
	return strings.HasPrefix(why, "F ") || strings.HasPrefix(why, "A ") || strings.HasPrefix(why, "D ")
}

// skippedJudgment is one package an extra build skipped, as the plain path judged it under the test's hook
// (pipeline.judgedSkipped): the build, the package's ID, which judgment, and why it declines, empty where it holds.
type skippedJudgment struct {
	build      string
	identifier string
	judgment   check.SkippedJudgment
	why        string
}

// skippedPackage is one package a load of an extra build skipped, as the test's hook was told it
// (pipeline.skippedAtLoad): the build and the package's ID.
type skippedPackage struct {
	build      string
	identifier string
}

// skippedRecorder gathers every load, every skip and every judgment the test's hooks are told, from whichever load's
// goroutine told it. loaded is the build of each load told, one for each.
type skippedRecorder struct {
	lock    sync.Mutex
	loaded  []string
	skipped []skippedPackage
	judged  []skippedJudgment
}

// hook has pipeline tell the recorder of every load of every extra build, of every skip of each and of its judgment
// (pipeline.extraBuildLoads, skippedAtLoad and judgedSkipped).
//
// Mutates: pipeline its three hooks are the recorder's
func (recorder *skippedRecorder) hook(pipeline *pipeline) {
	pipeline.extraBuildLoads = recorder.load
	pipeline.skippedAtLoad = recorder.skip
	pipeline.judgedSkipped = recorder.record
}

// load is the test's hook for a load (pipeline.extraBuildLoads).
func (recorder *skippedRecorder) load(build project.Build) {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	recorder.loaded = append(recorder.loaded, build.String())
}

// skip is the test's hook for a skip (pipeline.skippedAtLoad).
func (recorder *skippedRecorder) skip(build project.Build, identifier string) {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	recorder.skipped = append(recorder.skipped, skippedPackage{build: build.String(), identifier: identifier})
}

// record is the test's hook for a judgment (pipeline.judgedSkipped).
func (recorder *skippedRecorder) record(
	build project.Build,
	identifier string,
	judgment check.SkippedJudgment,
	why string,
) {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	recorder.judged = append(recorder.judged, skippedJudgment{build: build.String(), identifier: identifier,
		judgment: judgment, why: why})
}

// loads are the build of every load recorded so far, a slice of their own.
func (recorder *skippedRecorder) loads() []string {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	return slices.Clone(recorder.loaded)
}

// skips are every skip recorded so far, a slice of their own.
func (recorder *skippedRecorder) skips() []skippedPackage {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	return slices.Clone(recorder.skipped)
}

// judgments are every judgment recorded so far, a slice of their own.
func (recorder *skippedRecorder) judgments() []skippedJudgment {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	return slices.Clone(recorder.judged)
}

// skippedTally is what one extra build skipped and how its skips were judged, each count over every load, so the
// counts are of the same loads: loads, the loads themselves, the first and every one again, as the test's hook was
// told them; skipped, the skips its loads told of; judged, the judgments it made, of which byTypes are TypesHold's;
// and declined, the judgments that declined. lastLoad is the skips its last load recorded (skippedOf), a part of
// skipped.
type skippedTally struct {
	loads    int
	skipped  int
	lastLoad int
	judged   int
	byTypes  int
	declined int
}

// judgeSkips fails the test naming every judgment of judged that declined, every package a build of pipeline skipped in
// more loads than it was judged in, so a hook that judged nothing proves nothing, and every package a build's last
// load recorded as skipped (skippedOf) that no load told of. It returns each extra build's tally, by build, its loads
// counted from loaded, the build of each load told.
func judgeSkips(
	test *testing.T,
	pipeline *pipeline,
	loaded []string,
	skipped []skippedPackage,
	judged []skippedJudgment,
) map[string]skippedTally {
	test.Helper()
	tallies := map[string]skippedTally{}
	for _, build := range loaded {
		tally := tallies[build]
		tally.loads++
		tallies[build] = tally
	}
	runs := map[string]buildRun{}
	for _, run := range pipeline.buildRuns[1:] {
		runs[run.build.String()] = run
	}
	skips, judgments := map[skippedPackage]int{}, map[skippedPackage]int{}
	for _, skip := range skipped {
		tally := tallies[skip.build]
		tally.skipped++
		tallies[skip.build] = tally
		skips[skip]++
	}
	for _, judgment := range judged {
		tally := tallies[judgment.build]
		tally.judged++
		if judgment.judgment == check.SkippedByTypes {
			tally.byTypes++
		}
		judgments[skippedPackage{build: judgment.build, identifier: judgment.identifier}]++
		if judgment.why != "" && declinedAtCheck(runs[judgment.build], judgment.identifier) {
			test.Logf("%s: TypesHold declines %s, which the check declined and the build checked from source: %s",
				judgment.build, judgment.identifier, judgment.why)
		} else if judgment.why != "" {
			tally.declined++
			test.Errorf("%s: skipped %s, which %s declines: %s", judgment.build, judgment.identifier,
				judgment.judgment, judgment.why)
		}
		tallies[judgment.build] = tally
	}
	for _, skip := range slices.SortedFunc(maps.Keys(skips), compareSkippedPackages) {
		if judgments[skip] < skips[skip] {
			test.Errorf("%s: skipped %s in %d loads and judged it in %d, so the differential proves nothing of the "+
				"rest", skip.build, skip.identifier, skips[skip], judgments[skip])
		}
	}
	for _, run := range pipeline.buildRuns[1:] {
		name := run.build.String()
		tally := tallies[name]
		for _, identifier := range skippedOf(run) {
			tally.lastLoad++
			if skips[skippedPackage{build: name, identifier: identifier}] == 0 {
				test.Errorf("%s: its last load skipped %s, which no load told of, so it was never judged", name,
					identifier)
			}
		}
		tallies[name] = tally
	}
	return tallies
}

// compareSkippedPackages orders skips by build, then by package ID.
func compareSkippedPackages(first skippedPackage, second skippedPackage) int {
	if order := strings.Compare(first.build, second.build); order != 0 {
		return order
	}
	return strings.Compare(first.identifier, second.identifier)
}

// logTallies logs a line for every extra build pipeline ran, zeros included: its tally, the loads first, which are the
// test's own count, and then what the run's record says of its restore from the host's run (check.HostRestoreCounts),
// each said as the load it is of. Restored is the last load's, none once the restore is off. Visited is the last load's
// that had the restore on, since a load with it off, which a second round of declines leaves it, counts nothing anew.
// Not restored are the packages never made candidates or declined, by the first word of why, the load's over that
// same load and check's over every load. How many times the build loaded again is not read from that record, which
// counts only the loads its own declines asked for. A build whose load held type errors analyzes nothing after it
// (extraBuildRun returns with an empty result), so its record says nothing of its last load, and the line says why in
// place of those figures, rather than read "0 of them by the last" beside the skips its load told of. It fails naming
// every build with a line that was told of no load:
// a build pipeline ran loaded, so the hook is not wired and nothing was counted. It fails naming every build the census
// needs beside the host's that has no line, since a build the differential says nothing of cannot be told apart from
// one it never judged.
func logTallies(test *testing.T, label string, pipeline *pipeline, tallies map[string]skippedTally) {
	test.Helper()
	lined := map[string]bool{}
	for _, run := range pipeline.buildRuns[1:] {
		tally := tallies[run.build.String()]
		var fromHost check.HostRestoreCounts
		if run.result != nil {
			fromHost = run.result.FromHost
		}
		if len(run.typeErrors) > 0 {
			test.Logf("%s, %s: loads %d, %d skipped over every load, %d judged (%d by TypesHold), %d declined; its "+
				"load held type errors, %d of them, so it analyzed nothing and its record says nothing of its last load",
				label, run.build, tally.loads, tally.skipped, tally.judged, tally.byTypes, tally.declined,
				len(run.typeErrors))
		} else {
			test.Logf("%s, %s: loads %d, %d skipped over every load, %d of them by the last, %d judged (%d by "+
				"TypesHold), %d declined; the last load restored %d, the last with the restore on visited %d, not "+
				"restored by %v", label, run.build, tally.loads, tally.skipped, tally.lastLoad, tally.judged,
				tally.byTypes, tally.declined, len(fromHost.Restored), fromHost.Visited, fromHost.DeclinedBy)
		}
		if tally.loads == 0 {
			test.Errorf("%s, %s: was told of no load: the hook is not wired, so the line counts nothing of its loads",
				label, run.build)
		}
		lined[run.build.String()] = true
	}
	for _, build := range pipeline.described.builds[1:] {
		if !lined[build.String()] {
			test.Errorf("%s, %s: the census needs this build and the differential has no line for it, so a build it "+
				"never judged reads as one that skipped nothing", label, build)
		}
	}
}

// packageDifferentialFixtures are the fixtures with extra builds of the walk (TestTheWalkHoldsTheMergeOfEveryFixture)
// and of block 1 (build-dependence), by directory under testdata, the exit each run makes, how many times each of its
// extra builds loads, and, for a fixture whose builds skip nothing, why that is right, empty for one that must skip
// and judge some package (the floor). A build loads once, and once more for each round of packages its check declines:
// walk-reload's, walk-rejudge's and restore-from-host's decline one round, and walk-redecline's two, the second of
// which turns its restore off, so its third load skips nothing. Every fixture's builds restore some package unchecked,
// untouched or let through by its decision (#mtrjfyn), so every fixture must judge one.
var packageDifferentialFixtures = []struct {
	fixture      string
	wantExit     int
	wantLoads    int
	skipsNothing string
}{
	{fixture: "walk-square", wantExit: exitFindings, wantLoads: 1},
	{fixture: "walk-reload", wantExit: exitFindings, wantLoads: 2},
	{fixture: "walk-rejudge", wantExit: exitFindings, wantLoads: 2},
	{fixture: "walk-redecline", wantExit: exitFindings, wantLoads: 3},
	{fixture: "restore-from-host", wantExit: exitFindings, wantLoads: 2},
	{fixture: "restore-instance", wantExit: exitFindings, wantLoads: 1},
	{fixture: "restore-bodiless", wantExit: exitClean, wantLoads: 1},
	{fixture: "other-builds", wantExit: exitFindings, wantLoads: 1},
	{fixture: "build-dependence", wantExit: exitClean, wantLoads: 1},
}

// Over every fixture of the walk and of block 1 with extra builds, under `--no-cache`, which checks every build, each
// package an extra build restores unchecked is judged by TypesHold, and the differential fails naming each
// the judgment declines, by build, package, judgment and why. Each package a build's last load skipped must have been
// judged, and some build of the fixture must have judged one by TypesHold, the floor, so a fixture whose builds stop
// skipping fails rather than pass judging nothing, unless it says why it skips nothing. Each extra build loads as many
// times as its fixture says, by the test's own count of the loads it was told, a load that skips nothing among them.
// The hook changes nothing of the run: the stream, restore, findings and counts with it, normalized, are the run's
// without it.
func TestABuildSkipsNothingTypesHoldDeclines(test *testing.T) {
	test.Parallel()

	for _, step := range packageDifferentialFixtures {
		test.Run(step.fixture, func(test *testing.T) {
			test.Parallel()
			// The differential judges what each build checks from source, the standard library's packages among
			// them, so every run computes them rather than restore them from the template.
			coldSystemCacheFor(test)

			fixture := filepath.Join("testdata", step.fixture)
			recorder := &skippedRecorder{}
			root := copyFixture(test, fixture)
			pipeline, judgedRaw := runOverConfigured(test, root, step.wantExit, recorder.hook, "--no-cache")
			if len(pipeline.buildRuns) < 2 {
				test.Fatalf("the run checked %d builds, want an extra build beside the host's", len(pipeline.buildRuns))
			}
			tallies := judgeSkips(test, pipeline, recorder.loads(), recorder.skips(), recorder.judgments())
			logTallies(test, step.fixture, pipeline, tallies)
			for _, run := range pipeline.buildRuns[1:] {
				if loads := tallies[run.build.String()].loads; loads != step.wantLoads {
					test.Errorf("%s: the test was told of %d loads, want %d", run.build, loads, step.wantLoads)
				}
			}
			byTypes := 0
			for _, tally := range tallies {
				byTypes += tally.byTypes
			}
			if byTypes == 0 && step.skipsNothing == "" {
				test.Error("no build judged a package it skipped by TypesHold, so the fixture proves nothing")
			}

			_, plain := runOver(test, copyFixture(test, fixture), step.wantExit)
			if judgedStream := normalize(judgedRaw, root); judgedStream != plain {
				test.Errorf("the hook changed the run's stream:\nwith it\n%s\nwithout it\n%s", judgedStream, plain)
			}
		})
	}
}

// The package differential reports a package a build's decision restored though TypesHold declines it (#mtrjfyn). On
// main no decision lets such a package through, so the miss is planted: under the differential's hook, walk-square's
// windows build lets u through at its load whatever its reads say (pipeline.forcedThrough), and the differential
// reports exactly one decline, u's, by TypesHold, for s2's Outer, whose Inner holds a lock on windows alone. A run
// with the option and no hook forces nothing: its stream is the plain run's.
func TestTheDifferentialReportsAPackageTheDecisionLetThroughWrongly(test *testing.T) {
	test.Parallel()

	fixture := filepath.Join("testdata", "walk-square")
	recorder := &skippedRecorder{}
	runOverConfigured(test, copyFixture(test, fixture), exitFindings, func(pipeline *pipeline) {
		recorder.hook(pipeline)
		pipeline.forcedThrough = walkU
	}, "--no-cache")
	var declined []skippedJudgment
	for _, judgment := range recorder.judgments() {
		if judgment.why != "" {
			declined = append(declined, judgment)
		}
	}
	if len(declined) != 1 || !strings.HasPrefix(declined[0].build, "windows/") || declined[0].identifier != walkU ||
		declined[0].judgment != check.SkippedByTypes ||
		!strings.HasPrefix(declined[0].why, "T object "+walkS2+".Outer") {
		test.Errorf("the differential reported the declines %+v, want one, windows's of u by TypesHold, for s2's "+
			"Outer", declined)
	}

	forcedRoot := copyFixture(test, fixture)
	_, forced := runOverConfigured(test, forcedRoot, exitFindings,
		func(pipeline *pipeline) { pipeline.forcedThrough = walkU }, "--no-cache")
	_, plain := runOver(test, copyFixture(test, fixture), exitFindings)
	if normalize(forced, forcedRoot) != plain {
		test.Errorf("the option without the hook changed the run's stream:\nwith it\n%s\nwithout it\n%s",
			normalize(forced, forcedRoot), plain)
	}
}
