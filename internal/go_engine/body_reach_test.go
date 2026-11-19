package go_engine

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Under the body reach census, with `--other-builds --build-dependence`, windows's census of the body-reach fixture
// judges each of the host's bodies by what stage 1 recorded it using (#w72zxtq):
//   - PlatformName calls platformName, which a host-only file declares: reached by its name bit, Named.
//   - Positive converts to Number, whose underlying type alone differs: reached through ValueType's underlying bit,
//     which reading the name bit alone misses.
//   - Spinner takes holder.Spin, Holder gaining Spin on windows through platformPart, a windows file's type it
//     embeds: reached by the selection's receiver's underlying bit, Selected, before holder's own name bit, Named,
//     which ignoring the receiver leaves it to. That row is redundant today rather than untested: dropping it, or the
//     whole Selected row, only moves Spinner's reason, and leaves no body unreached for the differential to catch. The
//     use walk records every selector's operand's type as a ValueType use, which reads both bits of the type the
//     selection starts from, and a method set that moves through embedding sets that type's name bit by the
//     embedding's edge, which a Named use of the operand reads too. So the census pins the row by Spinner's reason,
//     and the differential's mutants are the Named row and the shadow rule.
//   - Size reads the universe's len, which windows declares at package level: reached, Shadowed, by no bit at all.
//   - Configured reads settings.A, whose identity is unknown: reached, Unknown, though no bit it reads is set.
//   - Gadget.Spin and Joined use nothing that differs: unreached, and the graph's unit verdict agrees on both.
//
// platformName's body in the host-only file is absent on windows, and windows's own platformName, Spin and len are
// new. The body differential compares the two unreached bodies' types, uses and findings on windows with the host's,
// and none differs. The census is written to its file, and the findings are the run's without it, which writes no
// census.
func TestTheBodyReachCensusJudgesEachOfTheHostsBodies(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	fromSource := test.TempDir()
	// A plain run records the packages each build checks from source, which the census then counts apart.
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		func(engine *pipeline) { engine.bodyReachFromSourceRecord = fromSource }, "--no-cache", "--no-fix",
		"--other-builds")
	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	on, onStream := runOverConfigured(test, root, exitFindings,
		func(engine *pipeline) {
			engine.bodyReachCensus = censusFile
			engine.bodyDifferential = true
			engine.bodyReachFromSource = fromSource
		}, "--no-cache", "--no-fix", "--other-builds",
		"--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		"--no-fix", "--other-builds", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	if on.buildRuns[0].bodyReach != nil {
		test.Error("the host's run has a census of its own, want none")
	}
	census := on.buildRuns[1].bodyReach
	if census == nil {
		test.Fatal("windows's run has no census")
	}
	got := map[string]string{}
	drivers := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier != "example.com/reach" {
			continue
		}
		drivers[body.name] = strings.ReplaceAll(body.driver, root+string(filepath.Separator), "")
		got[body.name] = string(body.reason)
		if body.reason == "" {
			got[body.name] = "unreached"
		}
		if body.reason == "" && body.unitDependent {
			test.Errorf("%s is unreached, and the graph's unit verdict calls it dependent", body)
		}
	}
	want := map[string]string{
		"PlatformName": "Named",
		"Positive":     "ValueType",
		"Spinner":      "Selected",
		"Size":         "Shadowed",
		"Configured":   "Unknown",
		"Gadget.Spin":  "unreached",
		"Joined":       "unreached",
	}
	if !maps.Equal(got, want) {
		test.Errorf("windows's census judges the host's bodies\n%v\nwant\n%v", got, want)
	}
	wantDrivers := map[string]string{
		"PlatformName": "example.com/reach.platformName (reach_other.go) Named name in the body",
		"Positive":     "example.com/reach.Number (reach.go) ValueType underlying in the body",
		"Spinner":      "example.com/reach.Holder (reach.go) Selected receiver underlying in the body",
		"Size":         "universe.len shadowed in the body",
		"Configured":   "an unknown identity in the body",
		"Gadget.Spin":  "",
		"Joined":       "",
	}
	if !maps.Equal(drivers, wantDrivers) {
		test.Errorf("windows's census says each body is reached first by\n%v\nwant\n%v", drivers, wantDrivers)
	}
	recorded, recordError := os.ReadFile(fromSourceRecordFile(fromSource, on.buildRuns[1].build))
	if recordError != nil || string(recorded) != "example.com/reach\n" {
		test.Errorf("the plain run recorded windows's from-source packages as %q (%v), want the one package",
			recorded, recordError)
	}
	// The plain run checks the one package from source on windows too, so the census within its record is the whole.
	within := on.buildRuns[1].bodyReachFromSource
	if within == nil || within.checkedFromSource != 1 || within.total != census.total ||
		within.reached != census.reached {
		test.Errorf("want the census within the plain run's from-source packages the whole, got %+v", within)
	}
	if census.total != 7 || census.reached != 5 || census.absent != 1 || census.new != 3 {
		test.Errorf("want 7 bodies, 5 reached, 1 absent and 3 new, got %d, %d, %d and %d", census.total,
			census.reached, census.absent, census.new)
	}
	if census.recorded != 1 || census.buildOnlyPackages != 0 || len(census.declarationsOnly) != 0 {
		test.Errorf("want the one package recorded, none the build's alone and none of declarations only, got %d, "+
			"%d and %v", census.recorded, census.buildOnlyPackages, census.declarationsOnly)
	}

	differential := on.buildRuns[1].bodyDifferential
	if differential == nil {
		test.Fatal("windows's run has no body differential")
	}
	if differential.compared != 2 || len(differential.failures) != 0 {
		test.Errorf("want the 2 unreached bodies compared with the host's and none differing, got %d compared and "+
			"the failures\n%s", differential.compared, strings.Join(differential.failures, "\n"))
	}

	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{
		"build " + on.buildRuns[1].build.String(),
		"  bodies: 7, reached 5, unreached 2, absent 1, new 3",
		"  unreached, and the graph's unit verdict calls dependent: 0",
		"  body differential: compared 2, failures 0",
	} {
		if !slices.Contains(strings.Split(string(written), "\n"), line) {
			test.Errorf("the census file has no line %q:\n%s", line, written)
		}
	}

	if off.bodyReachHost != nil || slices.ContainsFunc(off.buildRuns, func(run buildRun) bool {
		return run.bodyReach != nil
	}) {
		test.Error("a run without the census took one")
	}
	// Nor does the host's walk render a body for a differential that was never asked for.
	for _, reads := range off.buildRuns[0].reads {
		if _, isRendered := reads.BodyRenderings(); isRendered {
			test.Error("a run without the body differential rendered the host's bodies")
		}
	}
	if !slices.Equal(findingLines(normalize(onStream, root)), findingLines(offStream)) {
		test.Errorf("the findings with the census are\n%q\nwant those without it\n%q",
			findingLines(normalize(onStream, root)), findingLines(offStream))
	}
}
