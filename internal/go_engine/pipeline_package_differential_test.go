package go_engine

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// The package differential (#wcwx8vf): every package an extra build skips without judging is one the plain path's
// judgment of a visited package, TypesHold against the build's types, would hold. A build skips a package two ways,
// by TypesHold, which is the judgment itself, and by every other way of leaving it unjudged, which skippedOf names,
// today the walk's untouched (check's untouchedOf). Under the test's hook (pipeline.judgedSkipped) each of those is
// judged beside the run as a visited one is (check.CacheSession's JudgeSkipped), and the differential fails naming
// every one the judgment declines.

// skippedOf is the seam: the packages run, an extra build's, skipped over its last load without judging them by
// TypesHold, by ID. A later stage that skips a package another way adds its own skip here.
func skippedOf(run buildRun) []string {
	if run.result == nil {
		return nil
	}
	return run.result.FromHost.Untouched
}

// skippedJudgment is one package an extra build skipped, as the plain path judged it under the test's hook
// (pipeline.judgedSkipped): the build, the package's ID, which judgment, and why it declines, empty where it holds.
type skippedJudgment struct {
	build      string
	identifier string
	judgment   check.SkippedJudgment
	why        string
}

// skippedRecorder gathers every judgment the test's hook is told, from whichever load's goroutine judged it.
type skippedRecorder struct {
	lock   sync.Mutex
	judged []skippedJudgment
}

// record is the test's hook (pipeline.judgedSkipped).
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

// judgments are every judgment recorded so far, a slice of their own.
func (recorder *skippedRecorder) judgments() []skippedJudgment {
	recorder.lock.Lock()
	defer recorder.lock.Unlock()
	return slices.Clone(recorder.judged)
}

// skippedTally is what one extra build skipped and how its skips were judged: skipped, the packages its last load
// skipped (skippedOf); judged, the judgments it made over every load, of which byTypes are TypesHold's; and declined,
// the judgments that declined.
type skippedTally struct {
	skipped  int
	judged   int
	byTypes  int
	declined int
}

// judgeSkips fails the test naming every judgment of judged that declined, and every package a build of engine
// skipped over its last load that no judgment of that build judged, so a hook that judged nothing proves nothing. It
// returns each extra build's tally, by build.
func judgeSkips(test *testing.T, engine *pipeline, judged []skippedJudgment) map[string]skippedTally {
	test.Helper()
	tallies := map[string]skippedTally{}
	judgedIn := map[string]bool{}
	for _, judgment := range judged {
		tally := tallies[judgment.build]
		tally.judged++
		if judgment.judgment == check.SkippedByTypes {
			tally.byTypes++
		}
		judgedIn[judgment.build+" "+judgment.identifier] = true
		if judgment.why != "" {
			tally.declined++
			test.Errorf("%s: skipped %s, which %s declines: %s", judgment.build, judgment.identifier,
				judgment.judgment, judgment.why)
		}
		tallies[judgment.build] = tally
	}
	for _, run := range engine.buildRuns[1:] {
		name := run.build.String()
		tally := tallies[name]
		for _, identifier := range skippedOf(run) {
			tally.skipped++
			if !judgedIn[name+" "+identifier] {
				test.Errorf("%s: skipped %s and never judged it, so the differential proves nothing of it", name,
					identifier)
			}
		}
		tallies[name] = tally
	}
	return tallies
}

// logTallies logs a line for every extra build engine ran, zeros included: its tally, with what its last load visited
// and restored from the host's run, and every package it never made a candidate or declined, by the first word of
// why. It fails naming every build the census needs beside the host's that has no line, since a build the
// differential says nothing of cannot be told apart from one it never judged.
func logTallies(test *testing.T, label string, engine *pipeline, tallies map[string]skippedTally) {
	test.Helper()
	lined := map[string]bool{}
	for _, run := range engine.buildRuns[1:] {
		tally := tallies[run.build.String()]
		var fromHost check.HostRestoreCounts
		if run.result != nil {
			fromHost = run.result.FromHost
		}
		test.Logf("%s, %s: %d skipped by the last load, %d judged (%d by TypesHold), %d declined; the last load "+
			"visited %d and restored %d, not restored by %v", label, run.build, tally.skipped, tally.judged,
			tally.byTypes, tally.declined, fromHost.Visited, len(fromHost.Restored), fromHost.DeclinedBy)
		lined[run.build.String()] = true
	}
	for _, build := range engine.described.builds[1:] {
		if !lined[build.String()] {
			test.Errorf("%s, %s: the census needs this build and the differential has no line for it, so a build it "+
				"never judged reads as one that skipped nothing", label, build)
		}
	}
}

// packageDifferentialFixtures are the fixtures with extra builds of the walk (TestTheWalkHoldsTheMergeOfEveryFixture)
// and of block 1 (build-dependence), by directory under testdata, the exit each run makes, and, for a fixture whose
// builds skip nothing, why that is right, empty for one that must skip and judge some package (the floor).
var packageDifferentialFixtures = []struct {
	fixture      string
	wantExit     int
	skipsNothing string
}{
	{fixture: "walk-square", wantExit: exitFindings},
	{fixture: "walk-reload", wantExit: exitFindings},
	{fixture: "walk-rejudge", wantExit: exitFindings},
	{fixture: "walk-redecline", wantExit: exitFindings,
		skipsNothing: "windows visits every package it could restore, each reading platform or one visited for it"},
	{fixture: "restore-from-host", wantExit: exitFindings,
		skipsNothing: "windows visits every package it could restore, each reading platform or one visited for it"},
	{fixture: "restore-instance", wantExit: exitFindings},
	{fixture: "restore-bodiless", wantExit: exitClean},
	{fixture: "other-builds", wantExit: exitFindings},
	{fixture: "build-dependence", wantExit: exitClean},
}

// Over every fixture of the walk and of block 1 with extra builds, under `--no-cache --other-builds`, each package an
// extra build skips without judging is judged as a visited one is, and the differential fails naming each the
// judgment declines, by build, package, judgment and why. Each package a build's last load skipped must have been
// judged, and some build of the fixture must have judged one by TypesHold, the floor, so a fixture whose builds stop
// skipping fails rather than pass judging nothing, unless it says why it skips nothing. The hook changes nothing of
// the run: the stream, restore, findings and counts with it, normalized, are the run's without it.
func TestABuildSkipsNothingTypesHoldDeclines(test *testing.T) {
	test.Parallel()

	for _, step := range packageDifferentialFixtures {
		test.Run(step.fixture, func(test *testing.T) {
			test.Parallel()

			fixture := filepath.Join("testdata", step.fixture)
			recorder := &skippedRecorder{}
			root := copyFixture(test, fixture)
			engine, judgedRaw := runOverConfigured(test, root, step.wantExit,
				func(engine *pipeline) { engine.judgedSkipped = recorder.record }, "--no-cache", "--other-builds")
			if len(engine.buildRuns) < 2 {
				test.Fatalf("the run checked %d builds, want an extra build beside the host's", len(engine.buildRuns))
			}
			judged := recorder.judgments()
			tallies := judgeSkips(test, engine, judged)
			logTallies(test, step.fixture, engine, tallies)
			byTypes := 0
			for _, tally := range tallies {
				byTypes += tally.byTypes
			}
			if byTypes == 0 && step.skipsNothing == "" {
				test.Error("no build judged a package it skipped by TypesHold, so the fixture proves nothing")
			}

			_, plain := runOver(test, copyFixture(test, fixture), step.wantExit, "--other-builds")
			if judgedStream := normalize(judgedRaw, root); judgedStream != plain {
				test.Errorf("the hook changed the run's stream:\nwith it\n%s\nwithout it\n%s", judgedStream, plain)
			}
		})
	}
}

// packageDifferentialRootVariable names a tree the package differential runs over, skipped when it is unset:
//
//	COHERE_VERDICT_DIFFERENTIAL_ROOT=<tree> go run ./command/cohere-dev test ./internal/go_engine/ \
//	    -run 'TestABuildSkipsNothingTypesHoldDeclinesInARealTree$' -v
const packageDifferentialRootVariable = "COHERE_VERDICT_DIFFERENTIAL_ROOT"

// The package differential over the tree packageDifferentialRootVariable names, run in place under `--no-fix`, so
// nothing of the tree is written, with the findings cache off, under `--other-builds`, every package an extra build
// skips judged as a visited one is. It logs, for each extra build, the packages its last load skipped, the judgments
// made and those declined, and fails naming every decline and every skipped package never judged. A tree whose census
// needs no extra build is skipped.
func TestABuildSkipsNothingTypesHoldDeclinesInARealTree(test *testing.T) {
	test.Parallel()

	root := os.Getenv(packageDifferentialRootVariable)
	if root == "" {
		test.Skipf("%s names no tree", packageDifferentialRootVariable)
	}
	recorder := &skippedRecorder{}
	engine, _, standardError, exitCode := runOverExiting(test, root, rules.All(),
		func(engine *pipeline) { engine.judgedSkipped = recorder.record }, "--no-cache", "--no-fix", "--other-builds")
	if exitCode != exitClean && exitCode != exitFindings {
		test.Fatalf("the run over %s exited %d:\n%s", root, exitCode, standardError)
	}
	if len(engine.buildRuns) < 2 {
		test.Skipf("the census of %s needs no extra build", root)
	}
	tallies := judgeSkips(test, engine, recorder.judgments())
	logTallies(test, root, engine, tallies)
	judgedByTypes := 0
	for _, tally := range tallies {
		judgedByTypes += tally.byTypes
	}
	if judgedByTypes == 0 {
		test.Errorf("no build of %s skipped a package TypesHold judged, so the differential proves nothing of it",
			root)
	}
}
