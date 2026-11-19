package go_engine

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/project"
)

// Under the body reach census, with `--other-builds --build-dependence`, windows's census of the body-reach fixture
// judges each of the host's bodies by what stage 1 recorded it using (#w72zxtq):
//   - PlatformName calls platformName, which a host-only file declares: reached by its name bit, Named.
//   - Accepted and Shifted take a Number, whose underlying type alone differs: reached by their signatures, through
//     ValueType's underlying bit, though Accepted's block reads nothing of it.
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
//   - Configured reads settings.A, whose identity is unknown: reached, Unknown, though no bit it reads is set, and
//     Paired sizes an array in its signature by settings.B: Unknown by its signature.
//   - Parted selects box.Part, a field whose type is each build's own: Selected by the field's own name bit, before
//     Box's underlying bit, which the field sets too.
//   - Limit reads platformLimit, a constant each build's own file declares: Constant, before Named.
//   - Typed instantiates Sized with Number: the type argument is also a type expression, recorded as ValueType, which
//     reads the same bits first, so it is ValueType; Sized itself is unreached.
//   - Bytes takes a byte, which windows declares at package level: Shadowed by its signature.
//   - linked names a symbol by go:linkname, so the host records no function uses of it: its one body is reached,
//     Unrecorded.
//   - After, which a //line directive places in reach.rl, a file no build lists, calls platformName: reached, Named,
//     its file decided by the file as parsed, so it is neither counted absent nor left out of the count.
//   - Gadget.Spin, Joined, High, Before and Sized use nothing that differs: unreached, and the graph's unit verdict
//     agrees on each. High's shift reads the word size, which windows's arm64 shares with the host.
//
// platformName's and platformLimit's bodies in the host-only file are absent on windows, and windows's own
// platformName, platformLimit, Spin and len are new. The body differential compares the five unreached bodies' types,
// uses and findings on windows with the host's, and none differs. The census is written to its file, and the findings
// are the run's without it, which writes no census.
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
		"High":         "unreached",
		"Before":       "unreached",
		"After":        "Named",
		"Accepted":     "Signature",
		"Shifted":      "Signature",
		"Paired":       "Unknown",
		"Parted":       "Selected",
		"Limit":        "Constant",
		"Typed":        "ValueType",
		"Sized":        "unreached",
		"Bytes":        "Shadowed",
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
		"High":         "",
		"Before":       "",
		"After":        "example.com/reach.platformName (reach_other.go) Named name in the body",
		"Accepted":     "example.com/reach.Number (reach.go) ValueType underlying in the signature",
		"Shifted":      "example.com/reach.Number (reach.go) ValueType underlying in the signature",
		"Paired":       "an unknown identity in the signature",
		"Parted":       "example.com/reach.Box.UF0 (reach.go) Selected name in the body",
		"Limit":        "example.com/reach.platformLimit (reach_other.go) Constant name in the body",
		"Typed":        "example.com/reach.Number (reach.go) ValueType underlying in the body",
		"Sized":        "",
		"Bytes":        "universe.byte shadowed in the signature",
	}
	if !maps.Equal(drivers, wantDrivers) {
		test.Errorf("windows's census says each body is reached first by\n%v\nwant\n%v", drivers, wantDrivers)
	}
	recorded, recordError := os.ReadFile(fromSourceRecordFile(fromSource, on.buildRuns[1].build))
	if recordError != nil || string(recorded) != "example.com/reach\n" {
		test.Errorf("the plain run recorded windows's from-source packages as %q (%v), want the one package",
			recorded, recordError)
	}
	// The plain run checks reach alone from source on windows, restoring linked, so the census within its record is
	// the whole but for linked's one body.
	within := on.buildRuns[1].bodyReachFromSource
	if within == nil || within.checkedFromSource != 1 || within.total != census.total-1 ||
		within.reached != census.reached-1 {
		test.Errorf("want the census within the plain run's from-source packages the whole but linked, got %+v",
			within)
	}
	if census.total != 19 || census.reached != 14 || census.absent != 2 || census.new != 4 {
		test.Errorf("want 19 bodies, 14 reached, 2 absent and 4 new, got %d, %d, %d and %d", census.total,
			census.reached, census.absent, census.new)
	}
	// linked, and unsafe, which it imports and which holds no body, are unrecorded.
	if census.recorded != 1 || census.checkedFromSource != 3 || census.byReason[reachedUnrecorded] != 1 ||
		census.buildOnlyPackages != 0 || len(census.declarationsOnly) != 0 || len(census.miscounted) != 0 {
		test.Errorf("want reach recorded and linked's one body unrecorded, none the build's alone, none of "+
			"declarations only and none miscounted, got %d of %d recorded, %d unrecorded, %d, %v and %v",
			census.recorded, census.checkedFromSource, census.byReason[reachedUnrecorded], census.buildOnlyPackages,
			census.declarationsOnly, census.miscounted)
	}
	for _, body := range census.bodies {
		if body.name == "After" && filepath.Base(body.file) != "reach.rl" {
			test.Errorf("After is reported in %s, want the file its //line directive names", body.file)
		}
	}

	differential := on.buildRuns[1].bodyDifferential
	if differential == nil {
		test.Fatal("windows's run has no body differential")
	}
	if differential.compared != 5 || len(differential.failures) != 0 {
		test.Errorf("want the 5 unreached bodies compared with the host's and none differing, got %d compared and "+
			"the failures\n%s", differential.compared, strings.Join(differential.failures, "\n"))
	}

	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{
		"build " + on.buildRuns[1].build.String(),
		"  bodies: 19, reached 14, unreached 5, absent 2, new 4",
		"  unreached, and the graph's unit verdict calls dependent: 0",
		"  body differential: compared 5, failures 0",
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

// The body differential is the machine every skip leans on, so it is shown failing (#w72zxtq, the review's must 9):
// handed a census that leaves unreached six bodies the reach rightly reaches, it fails each by what it compares.
// PlatformName's call resolves to another declaring file, and Size's len to windows's own function, of another type;
// Positive's conversion to Number reads apart by
// Number's whole lines, whose underlying type differs, though both builds print the type alike; Configured's
// settings.A has no identity, which matches none, another unknown's included; Accepted differs only in its signature,
// which is rendered with its block; and Shifted's findings differ, vet reporting its shift where Number is an int32.
// Handed a census that reaches every body, it compares none, and fails for it.
func TestTheBodyDifferentialFailsWhereTheReachWronglyLeavesABody(test *testing.T) {
	test.Parallel()

	wronglyUnreached := []string{"PlatformName", "Size", "Positive", "Configured", "Accepted", "Shifted"}
	run := func(census func(*buildBodyReach) *buildBodyReach) *buildBodyDifferential {
		test.Helper()
		on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
			func(engine *pipeline) {
				engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				engine.bodyDifferential = true
				engine.bodyDifferentialCensus = census
			}, "--no-cache", "--no-fix", "--other-builds", "--build-dependence")
		if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
			test.Fatal("want windows's body differential")
		}
		return on.buildRuns[1].bodyDifferential
	}
	differential := run(func(census *buildBodyReach) *buildBodyReach {
		altered := *census
		altered.bodies = slices.Clone(census.bodies)
		for index, body := range altered.bodies {
			if slices.Contains(wronglyUnreached, body.name) {
				altered.bodies[index].reason = ""
			}
		}
		return &altered
	})
	if differential.compared != 11 {
		test.Errorf("want 11 bodies compared, the 5 the reach leaves and the 6 it was told to, got %d",
			differential.compared)
	}
	failed := map[string][]string{}
	for _, failure := range differential.failures {
		for _, name := range wronglyUnreached {
			if strings.Contains(failure, " "+name+": ") {
				failed[name] = append(failed[name], failure)
			}
		}
	}
	for name, wanted := range map[string]string{
		"PlatformName": "use example.com/reach.platformName (",
		"Size":         "type func(values []int) int",
		"Positive":     "type example.com/reach.Number example.com/reach.Number{",
		"Configured":   "an unknown identity matching none",
		"Accepted":     "type example.com/reach.Number example.com/reach.Number{",
		"Shifted":      "findings differ: host [",
	} {
		if !slices.ContainsFunc(failed[name], func(failure string) bool { return strings.Contains(failure, wanted) }) {
			test.Errorf("want %s failed by %q, got %q", name, wanted, failed[name])
		}
	}
	if len(differential.failures) != len(wronglyUnreached)+1 {
		test.Errorf("want a failure for each body wrongly left unreached and a second for Shifted, got\n%s",
			strings.Join(differential.failures, "\n"))
	}

	none := run(func(census *buildBodyReach) *buildBodyReach {
		altered := *census
		altered.bodies = slices.Clone(census.bodies)
		for index := range altered.bodies {
			altered.bodies[index].reason = reachedNamed
		}
		return &altered
	})
	if none.compared != 0 || len(none.failures) != 1 || !strings.HasPrefix(none.failures[0], "compared no body") {
		test.Errorf("want a census that reaches every body to compare none and fail for it, got %d compared and %q",
			none.compared, none.failures)
	}
}

// A build the census has no judgment of compares nothing, and the body differential fails it, in the census file
// and on stderr, rather than writing nothing for it (#w72zxtq).
func TestABuildWithNoJudgmentFailsTheBodyDifferential(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	var notes strings.Builder
	engine := &pipeline{bodyReachHost: &bodyReachHost{}, bodyDifferential: true, bodyReachCensus: censusFile,
		standardError: &notes}
	engine.writeBodyReachCensus([]buildRun{{build: project.Build{OperatingSystem: "darwin"}},
		{build: project.Build{OperatingSystem: "windows"}}})
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{"  not judged: the build has no census",
		"  body differential: compared 0, failures 1"} {
		if !strings.Contains(string(written), line+"\n") {
			test.Errorf("the census file has no line %q:\n%s", line, written)
		}
	}
	if !strings.Contains(notes.String(), "the body differential of the build windows") {
		test.Errorf("want the failure noted on stderr, got %q", notes.String())
	}
}

// A build on another architecture checks every package under other sizes (#w72zxtq, the review's must 2), so
// `unsafe.Sizeof`, a shift past a word and `math.MaxInt` type-check to other constants there with no file differing
// and no bit set: on linux/386, a target the settings name, every body is reached by the sizes, High among them,
// whose shift by 32 is all of 386's word and half of arm64's, while windows on the host's architecture leaves High
// unreached as before. The census file counts the reason.
func TestABuildUnderOtherSizesReachesEveryBody(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	settingsFile := filepath.Join(root, "CohereSettings.json")
	if writeError := os.WriteFile(settingsFile, []byte(`{"go": {"rules": {}, "targets": ["linux/386"]}}`),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = censusFile
	}, "--no-cache", "--no-fix", "--other-builds", "--build-dependence")

	censuses := map[string]*buildBodyReach{}
	for _, run := range on.buildRuns[1:] {
		censuses[run.build.OperatingSystem+"/"+run.build.Architecture] = run.bodyReach
	}
	narrow := censuses["linux/386"]
	if narrow == nil {
		test.Fatalf("want a census of linux/386, got the builds %v", slices.Collect(maps.Keys(censuses)))
	}
	if narrow.total == 0 || narrow.reached != narrow.total || narrow.byReason[reachedSizes] != narrow.total {
		test.Errorf("want every body of linux/386 reached by its sizes, got %d of %d, %d by Sizes", narrow.reached,
			narrow.total, narrow.byReason[reachedSizes])
	}
	reasons := map[string]map[string]string{}
	for build, census := range censuses {
		reasons[build] = map[string]string{}
		for _, body := range census.bodies {
			if body.packageIdentifier == "example.com/reach" {
				reasons[build][body.name] = cmp.Or(string(body.reason), "unreached")
			}
		}
	}
	if reasons["linux/386"]["High"] != "Sizes" || reasons["windows/"+runtime.GOARCH]["High"] != "unreached" {
		test.Errorf("want High reached by the sizes on linux/386 and unreached on windows, got %v", reasons)
	}
	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	if !strings.Contains(string(written), fmt.Sprintf("  reached by reason: Sizes %d, ", narrow.total)) {
		test.Errorf("the census file counts no body of linux/386 reached by its sizes:\n%s", written)
	}
}

// On a warm run the host's graph takes a package the findings store holds a record of from that record, which holds
// no live object, so the reach asks of the package's objects by its ID and their names (bodyReacher.bits). A package
// beside its in-package test variant shares its path with it, so a name asked by its path names no one package and
// read nothing set (#w72zxtq, the review's must 1). Here reach has a test, so the host holds reach and its variant, and
// user, changed between the runs and so checked from source with a record of its own uses, calls reach's PlatformLabel,
// which each build's own file declares: Show is reached by its name bit through the decoded record, and Plain, calling
// Joined, which no build changes, is unreached.
func TestAWarmRunReachesABodyThroughADecodedPackageWithATestVariant(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	for name, contents := range map[string]string{
		"reach_test.go": `package reach

import "testing"

// The test makes reach's in-package test variant.
func TestJoined(test *testing.T) {
	if Joined("a", "b") != "a/b" {
		test.Fail()
	}
}
`,
		"label_other.go": `//go:build !windows

package reach

// PlatformLabel is every platform's but windows's.
func PlatformLabel() string {
	return "other"
}
`,
		"label_windows.go": `package reach

// PlatformLabel is windows's.
func PlatformLabel() int {
	return 1
}
`,
		filepath.Join("user", "user.go"): `// Package user calls reach.
package user

import "example.com/reach"

// Show calls a function each build's own file declares.
func Show() any {
	return reach.PlatformLabel()
}

// Plain calls one no build changes.
func Plain() string {
	return reach.Joined("a", "b")
}
`,
	} {
		if writeError := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); writeError != nil {
			test.Fatal(writeError)
		}
		if writeError := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	runOverConfigured(test, root, exitFindings, nil, "--no-fix", "--other-builds", "--build-dependence")
	// user changes, so the warm run checks it from source and records its uses, while reach's record is decoded.
	userFile := filepath.Join(root, "user", "user.go")
	contents, readError := os.ReadFile(userFile)
	if readError != nil {
		test.Fatal(readError)
	}
	if writeError := os.WriteFile(userFile, append(contents, "\n// Changed for the warm run.\n"...),
		0o644); writeError != nil {
		test.Fatal(writeError)
	}
	warm, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
	}, "--no-fix", "--other-builds", "--build-dependence")

	hosted, isHosted := warm.bodyReachHost.packages["example.com/reach"]
	if !isHosted || !warm.dependence.decoded[hosted.Types] {
		test.Fatal("want reach decoded on the warm run")
	}
	if _, isHosted := warm.bodyReachHost.packages["example.com/reach [example.com/reach.test]"]; !isHosted {
		test.Fatal("want the host to hold reach's test variant beside it")
	}
	if len(warm.buildRuns) != 2 || warm.buildRuns[1].bodyReach == nil {
		test.Fatalf("want windows's census, got %d runs", len(warm.buildRuns))
	}
	got := map[string]string{}
	for _, body := range warm.buildRuns[1].bodyReach.bodies {
		if body.packageIdentifier == "example.com/reach/user" {
			got[body.name] = cmp.Or(string(body.reason), "unreached")
		}
	}
	if want := map[string]string{"Show": "Named", "Plain": "unreached"}; !maps.Equal(got, want) {
		test.Errorf("windows's warm census judges user's bodies\n%v\nwant\n%v", got, want)
	}
}
