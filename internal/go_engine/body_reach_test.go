package go_engine

import (
	"cmp"
	"fmt"
	"go/types"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/system-inc/cohere/internal/go_engine/check"
	"github.com/system-inc/cohere/internal/go_engine/load"
	"github.com/system-inc/cohere/internal/go_engine/project"
)

// Under the body reach census, with `--build-dependence`, windows's census of the body-reach fixture
// judges each of the host's bodies by what stage 1 recorded it using (#w72zxtq):
//   - PlatformName calls platformName, which a host-only file declares: reached by its name bit, Named.
//   - Accepted and Shifted take a Number, whose underlying type alone differs: reached by their signatures, though
//     Accepted's block reads nothing of it. Number is defined from platformInteger's name, so the graph reads
//     platformInteger in full into Number's name bit as well as its underlying one (build_dependence's defined-from
//     edge, #9paaen9's block 1): the Named row reaches them first, by the name bit. The ValueType underlying row is
//     pinned on its own by body-reach-moved's bodies, whose types spell their structs.
//   - Positive converts to Number, reached the same way, by Number's name bit.
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
//   - Limit reads platformLimit, a constant each build's own file declares: Constant, before Named, and so do
//     limitTwice, declared from it, and Twice, reading limitTwice, a shared file's constant whose value differs.
//   - Typed instantiates Sized with Number: the type argument is recorded as ValueType as well as Named, and Named
//     reaches first by Number's name bit; Sized itself is unreached.
//   - Bytes takes a byte, which windows declares at package level: Shadowed by its signature.
//   - linked names a symbol by go:linkname, so the host records no function uses of it: its one body is reached,
//     Unrecorded.
//   - After, which a //line directive places in reach.rl, a file no build lists, calls platformName: reached, Named,
//     its file decided by the file as parsed, so it is neither counted absent nor left out of the count.
//   - Gadget.Spin, Joined, High, Before, Sized and Firsts use nothing that differs: unreached, and the graph's unit
//     verdict agrees on each. High's shift reads the word size, which windows's arm64 shares with the host, and
//     Firsts reads an instance's field, which the differential names by its generic's, as stage 1's record does.
//   - size calls platformSize, a host-only file's: Named. Warm calls size alone, and helper calls nothing: both
//     unreached, though their findings differ, by what the reach does not stand for. On windows platformSize is pure,
//     so SA4017 reports Warm's discarded call, a callee's fact, stage 4's; and windows's init calls helper, so U1000,
//     a package-wide rule, reports helper unused on the host alone, stage 5's, in a package holding dependent units.
//
// platformName's, platformLimit's and platformSize's bodies in the host-only file are absent on windows, and windows's
// own platformName, platformLimit, platformSize, Spin, len and init are new. The body differential compares the eight
// unreached bodies' types, uses and findings on windows with the host's: helper's findings are classed apart under
// stage 5, its package holding dependent units, and Warm's fail, owed to stage 4, since the build cannot yet show that
// size's purity differs (findingCauses, #5vbs1np). The census is written to its file, saying what the reach does not
// stand for, and the findings are the run's without it, which writes no census.
func TestTheBodyReachCensusJudgesEachOfTheHostsBodies(test *testing.T) {
	test.Parallel()

	censusFile := filepath.Join(test.TempDir(), "reach.txt")
	fromSource := test.TempDir()
	// A plain run records the packages each build checks from source, which the census then counts apart.
	runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		func(engine *pipeline) { engine.bodyReachFromSourceRecord = fromSource }, "--no-cache", "--no-fix")
	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	on, onStream := runOverConfigured(test, root, exitFindings,
		func(engine *pipeline) {
			engine.bodyReachCensus = censusFile
			engine.bodyDifferential = true
			engine.bodyReachFromSource = fromSource
		}, "--no-cache", "--no-fix", "--build-dependence")
	off, offStream := runOver(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
		"--no-fix", "--build-dependence")

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
		"Positive":     "Named",
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
		"limitTwice":   "Constant",
		"Twice":        "Constant",
		"Typed":        "Named",
		"Sized":        "unreached",
		"Bytes":        "Shadowed",
		"size":         "Named",
		"Warm":         "unreached",
		"helper":       "unreached",
		"Firsts":       "unreached",
	}
	if !maps.Equal(got, want) {
		test.Errorf("windows's census judges the host's bodies\n%v\nwant\n%v", got, want)
	}
	wantDrivers := map[string]string{
		"PlatformName": "example.com/reach.platformName (reach_other.go) Named name in the body",
		"Positive":     "example.com/reach.Number (reach.go) Named name in the body",
		"Spinner":      "example.com/reach.Holder (reach.go) Selected receiver underlying in the body",
		"Size":         "universe.len shadowed in the body",
		"Configured":   "an unknown identity in the body",
		"Gadget.Spin":  "",
		"Joined":       "",
		"High":         "",
		"Before":       "",
		"After":        "example.com/reach.platformName (reach_other.go) Named name in the body",
		"Accepted":     "example.com/reach.Number (reach.go) Named name in the signature",
		"Shifted":      "example.com/reach.Number (reach.go) Named name in the signature",
		"Paired":       "an unknown identity in the signature",
		"Parted":       "example.com/reach.Box.UF0 (reach.go) Selected name in the body",
		"Limit":        "example.com/reach.platformLimit (reach_other.go) Constant name in the body",
		"limitTwice":   "example.com/reach.platformLimit (reach_other.go) Constant name in the body",
		"Twice":        "example.com/reach.limitTwice (reach.go) Constant name in the body",
		"Typed":        "example.com/reach.Number (reach.go) Named name in the body",
		"Sized":        "",
		"Bytes":        "universe.byte shadowed in the signature",
		"size":         "example.com/reach.platformSize (reach_other.go) Named name in the body",
		"Warm":         "",
		"helper":       "",
		"Firsts":       "",
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
	if census.total != 25 || census.reached != 17 || census.absent != 3 || census.new != 6 {
		test.Errorf("want 25 bodies, 17 reached, 3 absent and 6 new, got %d, %d, %d and %d", census.total,
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
	wantFailures := []string{"example.com/reach reach.go:135 Warm: findings differ: host [], build [cohere-go/SA4017 " +
		"136:2 size doesn't have side effects and its return value is ignored], owed by its rules to CalleeFacts, " +
		"whose cause the build does not show"}
	var failures []string
	for _, failure := range differential.failures {
		failures = append(failures, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
	}
	if differential.compared != 8 || !slices.Equal(failures, wantFailures) {
		test.Errorf("want the 8 unreached bodies compared with the host's and Warm alone failing, got %d compared and "+
			"the failures\n%s", differential.compared, strings.Join(failures, "\n"))
	}
	classed := map[findingStage]string{}
	for stage, lines := range differential.classed {
		for _, line := range lines {
			classed[stage] += strings.ReplaceAll(line, root+string(filepath.Separator), "")
		}
	}
	if want := map[findingStage]string{
		stagePackageWide: "example.com/reach reach.go:141 helper: findings differ: host [cohere-go/U1000 141:6 func " +
			"helper is unused], build []",
	}; !maps.Equal(classed, want) {
		test.Errorf("the differential classes apart\n%v\nwant\n%v", classed, want)
	}

	written, readError := os.ReadFile(censusFile)
	if readError != nil {
		test.Fatal(readError)
	}
	for _, line := range []string{
		"build " + on.buildRuns[1].build.String(),
		"The reach stands for no finding that turns on a callee's fact, stage 4's (#h0k644s), nor for a package-wide " +
			"rule's, stage 5's (#xeafx79): the body differential classes a package-wide rule's findings apart where " +
			"the package holds a dependent unit, and fails a callee's fact's until the build can show one differs " +
			"(#5vbs1np).",
		"  bodies: 25, reached 17, unreached 8, absent 3, new 6",
		"  unreached, and the graph's unit verdict calls dependent: 0",
		"  body differential: compared 8, failures 1",
		"  findings differing turning on a callee's fact, stage 4's (#h0k644s): 0",
		"  findings differing of a package-wide rule, stage 5's (#xeafx79): 1",
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
// handed a census that leaves unreached seven bodies the reach rightly reaches, it fails each by what it compares.
// PlatformName's call resolves to another declaring file, and Size's len to windows's own function, of another type;
// Positive's conversion to Number reads apart by Number's whole lines, whose underlying type differs, though both
// builds print the type alike; Configured's settings.A has no identity, which matches none, another unknown's
// included; Accepted differs only in its signature, which is rendered with its block; Twice's limitTwice names one
// declaration in both, whose value differs; and Shifted's findings differ, vet reporting its shift where Number is an
// int32.
// Handed a census that reaches every body, it compares none, and fails for it.
func TestTheBodyDifferentialFailsWhereTheReachWronglyLeavesABody(test *testing.T) {
	test.Parallel()

	wronglyUnreached := []string{"PlatformName", "Size", "Positive", "Configured", "Accepted", "Twice", "Shifted"}
	run := func(census func(*buildBodyReach) *buildBodyReach) *buildBodyDifferential {
		test.Helper()
		on, _ := runOverConfigured(test, copyFixture(test, filepath.Join("testdata", "body-reach")), exitFindings,
			func(engine *pipeline) {
				engine.bodyReachCensus = filepath.Join(test.TempDir(), "reach.txt")
				engine.bodyDifferential = true
				engine.bodyDifferentialCensus = census
			}, "--no-cache", "--no-fix", "--build-dependence")
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
	if differential.compared != 15 {
		test.Errorf("want 15 bodies compared, the 8 the reach leaves and the 7 it was told to, got %d",
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
		"Twice":        "type int = 32",
		"Shifted":      "findings differ: host [",
	} {
		if !slices.ContainsFunc(failed[name], func(failure string) bool { return strings.Contains(failure, wanted) }) {
			test.Errorf("want %s failed by %q, got %q", name, wanted, failed[name])
		}
	}
	// Beside them, Warm, which the reach leaves unreached, fails by its findings, owed to stage 4, whose cause the
	// build cannot yet show (findingCauses).
	if len(differential.failures) != len(wronglyUnreached)+2 ||
		!slices.ContainsFunc(differential.failures, func(failure string) bool {
			return strings.Contains(failure, " Warm: findings differ: ")
		}) {
		test.Errorf("want a failure for each body wrongly left unreached, a second for Shifted and one for Warm, "+
			"got\n%s", strings.Join(differential.failures, "\n"))
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

// A named type's digest holds its method set beside its layout (check.NamedTypesWritten), so a body that holds a type
// whose method set alone differs reads apart (#w72zxtq, the review's mutant 5). Held takes and returns a Holder, whose
// layout names inner and platformPart alike in both builds, while windows's platformPart gives Holder its Spin at a
// shallower depth than inner's Gadget: handed to the differential as unreached, Held fails by Holder's digest.
func TestTheBodyDifferentialFailsABodyWhoseTypeDiffersByItsMethodSetAlone(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach"))
	if writeError := os.WriteFile(filepath.Join(root, "held.go"), []byte(`package reach

// Held hands on a Holder, whose method set alone differs on windows.
func Held(holder Holder) Holder {
	return holder
}
`), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyDifferential = true
		engine.bodyDifferentialCensus = func(census *buildBodyReach) *buildBodyReach {
			altered := *census
			altered.bodies = slices.Clone(census.bodies)
			for index, body := range altered.bodies {
				if body.name == "Held" {
					altered.bodies[index].reason = ""
				}
			}
			return &altered
		}
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
		test.Fatal("want windows's body differential")
	}
	var failed []string
	for _, failure := range on.buildRuns[1].bodyDifferential.failures {
		if strings.Contains(failure, " Held: ") {
			failed = append(failed, failure)
		}
	}
	if len(failed) != 1 || !strings.Contains(failed[0], "type example.com/reach.Holder example.com/reach.Holder{") {
		test.Errorf("want Held failed once by Holder's digest, got %q", failed)
	}
}

// A load checked under no sizes is never read as the host's sizes, which nothing says it shares (#w72zxtq, the
// review's mutant 6): either side with none differs, failing wide, and two loads under one measure do not.
func TestABuildCheckedUnderNoSizesDiffersFromTheHost(test *testing.T) {
	test.Parallel()

	loaded := func(sizes types.Sizes) *load.Loaded {
		return &load.Loaded{Roots: []*packages.Package{{ID: "example.com/sized", TypesSizes: sizes}}}
	}
	arm64 := types.SizesFor("gc", "arm64")
	for _, each := range []struct {
		name  string
		host  types.Sizes
		built types.Sizes
		want  bool
	}{
		{"the host under no sizes", nil, arm64, true},
		{"the build under no sizes", arm64, nil, true},
		{"both under arm64's", arm64, types.SizesFor("gc", "arm64"), false},
		{"the build under 386's", arm64, types.SizesFor("gc", "386"), true},
	} {
		if got := buildSizesDiffer(loaded(each.host), loaded(each.built)); got != each.want {
			test.Errorf("%s: the sizes differ %t, want %t", each.name, got, each.want)
		}
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
	}, "--no-cache", "--no-fix", "--build-dependence")

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

// A rule handed a value as an empty interface looks through the value's type past what its own bits say (#h0k644s):
// printf's verb match, SA1026's marshal walk and SA1003's binary walk each judge a type below a named slice, which
// reads its element by name alone. The body-reach-moved fixture hands each reader a named slice of a struct holding a
// type each build's own file declares, and a struct holding that slice, and neither bit of either is set on windows.
// The use walk records, for a value moved to an empty interface, a use of every named type a look through it reaches
// (useWalk.reachable), so windows's census reaches all six bodies through the entry struct's underlying bit, ValueType.
// Without that record none of the six is reached, and the body differential finds the three rules reporting on windows
// what the host never did in five of them, `binary.Write` refusing a struct that holds a slice in both builds alike.
// ShowPlain prints a named slice with nothing any build changes below it: unreached, and the same in both builds.
//
// The fixture imports the standard library, whose bodies the census counts too, so only the fixture's own are read.
func TestAValueMovedToAnEmptyInterfaceReachesItsBodyThroughTheTypesBelowIt(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach-moved"))
	on, stream := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyDifferential = true
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].build.OperatingSystem != "windows" {
		test.Fatalf("want the host's build and windows's, got %d runs", len(on.buildRuns))
	}
	census, differential := on.buildRuns[1].bodyReach, on.buildRuns[1].bodyDifferential
	if census == nil || differential == nil {
		test.Fatal("windows's run has no census or no body differential")
	}
	got := map[string]string{}
	drivers := map[string]string{}
	for _, body := range census.bodies {
		if body.packageIdentifier != "example.com/moved" {
			continue
		}
		got[body.name] = cmp.Or(string(body.reason), "unreached")
		drivers[body.name] = strings.ReplaceAll(body.driver, root+string(filepath.Separator), "")
	}
	want := map[string]string{
		"Show":          "ValueType",
		"ShowNested":    "ValueType",
		"Marshal":       "ValueType",
		"MarshalNested": "ValueType",
		"Write":         "ValueType",
		"WriteNested":   "ValueType",
		"ShowPlain":     "unreached",
	}
	if !maps.Equal(got, want) {
		test.Errorf("windows's census judges the fixture's bodies\n%v\nwant\n%v", got, want)
	}
	wantDrivers := map[string]string{
		"Show":          "example.com/moved.PrintedEntry (moved.go) ValueType underlying in the body",
		"ShowNested":    "example.com/moved.PrintedEntry (moved.go) ValueType underlying in the body",
		"Marshal":       "example.com/moved.MarshalledEntry (moved.go) ValueType underlying in the body",
		"MarshalNested": "example.com/moved.MarshalledEntry (moved.go) ValueType underlying in the body",
		"Write":         "example.com/moved.WrittenEntry (moved.go) ValueType underlying in the body",
		"WriteNested":   "example.com/moved.WrittenEntry (moved.go) ValueType underlying in the body",
		"ShowPlain":     "",
	}
	if !maps.Equal(drivers, wantDrivers) {
		test.Errorf("windows's census says each body is reached first by\n%v\nwant\n%v", drivers, wantDrivers)
	}

	var compared []string
	for _, each := range differential.bodies {
		if each.body.packageIdentifier == "example.com/moved" {
			compared = append(compared, each.body.name)
		}
	}
	if !slices.Equal(compared, []string{"ShowPlain"}) {
		test.Errorf("want ShowPlain alone of the fixture's bodies compared with the host's, got %v", compared)
	}
	for _, failure := range differential.failures {
		if strings.HasPrefix(failure, "example.com/moved ") {
			test.Errorf("a body of the fixture left unreached differs on windows: %s", failure)
		}
	}
	// The fixture's premise: each of the three readers reports on the named slice, which windows's types alone draw.
	for _, message := range []string{
		"fmt.Printf format %d has arg entries of wrong type example.com/moved.PrintedEntries",
		"trying to marshal unsupported type marshalledValue, via x[0].Value",
		"value of type example.com/moved.WrittenEntries cannot be used with binary.Write",
	} {
		if !strings.Contains(stream, `"message":"`+message+`"`) {
			test.Errorf("want the run to report %q, and it does not", message)
		}
	}
}

// A findings difference is excused under a later stage only where the build shows that stage's cause (#w72zxtq, the
// review's must B), never by its rule's name alone. shown's Show prints moved's PrintedEntries, and printf, looking
// through the slice to printedValue, reports it on windows alone. The types the differential renders read alike in
// both builds, PrintedEntries's digest naming PrintedEntry by name one level down, so only the findings differ, and
// printf both reads facts and judges its package whole. Neither cause holds: Show calls fmt.Printf, whose facts no
// build changes, and shown holds no unit windows judges dependent. Handed to the differential as unreached, Show is a
// failure by its findings, not a class, where a rule's name alone would have classed it a callee's fact's.
func TestTheBodyDifferentialClassesNoDifferenceWhoseCauseItCannotSee(test *testing.T) {
	test.Parallel()

	root := copyFixture(test, filepath.Join("testdata", "body-reach-moved"))
	if writeError := os.MkdirAll(filepath.Join(root, "shown"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	if writeError := os.WriteFile(filepath.Join(root, "shown", "shown.go"), []byte(`// Package shown prints moved's
// named slice, in a package of its own that holds nothing any build changes.
package shown

import (
	"fmt"

	"example.com/moved"
)

// Show prints a named slice, which printf looks through to moved's printedValue.
func Show(entries moved.PrintedEntries) {
	fmt.Printf("%d\n", entries)
}
`), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	on, _ := runOverConfigured(test, root, exitFindings, func(engine *pipeline) {
		engine.bodyDifferential = true
		engine.bodyDifferentialCensus = func(census *buildBodyReach) *buildBodyReach {
			altered := *census
			altered.bodies = slices.Clone(census.bodies)
			for index, body := range altered.bodies {
				if body.packageIdentifier == "example.com/moved/shown" && body.name == "Show" {
					altered.bodies[index].reason = ""
				}
			}
			return &altered
		}
	}, "--no-cache", "--no-fix", "--build-dependence")

	if len(on.buildRuns) != 2 || on.buildRuns[1].bodyDifferential == nil {
		test.Fatal("want windows's body differential")
	}
	differential := on.buildRuns[1].bodyDifferential
	isShow := func(line string) bool { return strings.HasPrefix(line, "example.com/moved/shown ") }
	var failed []string
	for _, failure := range differential.failures {
		if isShow(failure) {
			failed = append(failed, strings.ReplaceAll(failure, root+string(filepath.Separator), ""))
		}
	}
	for stage, lines := range differential.classed {
		for _, line := range lines {
			if isShow(line) {
				test.Errorf("Show's difference is classed %s: %s", stage, line)
			}
		}
	}
	// Both builds report the call's console write and its dropped error alike; printf's finding is windows's alone.
	for _, wanted := range []string{"example.com/moved/shown shown/shown.go:12 Show: findings differ: host [",
		"; cohere-go/printf 13:14 fmt.Printf format %d has arg entries of wrong type example.com/moved.PrintedEntries]",
	} {
		if len(failed) != 1 || !strings.Contains(failed[0], wanted) {
			test.Errorf("want Show failed once by its findings, with %q, got\n%s", wanted, strings.Join(failed, "\n"))
		}
	}
}

// A body is classed under a later stage only where one stage owns every finding that differs and the build shows its
// cause (#w72zxtq, the review's mutant 4): a body mixing a difference stage 5 would excuse, its package holding a
// dependent unit, with one no stage owns fails, whichever finding sorts first, and so does one whose differences two
// stages own, stage 4 excusing nothing yet. One whose every difference is stage 5's is classed there. Each body is
// alone in a file of its own, its findings on its own lines.
func TestABodyIsClassedOnlyWhereOneStageOwnsEveryDifference(test *testing.T) {
	test.Parallel()

	stages := map[string][]findingStage{"facts": {stageCalleeFacts}, "whole": {stagePackageWide},
		"both": {stageCalleeFacts, stagePackageWide}}
	cases := []struct {
		name  string
		host  []string
		built []string
		want  findingStage
	}{
		{"an unowned difference sorted before an excused one", nil, []string{"alone", "whole"}, ""},
		{"an excused difference sorted before an unowned one", nil, []string{"whole", "plain"}, ""},
		{"an unowned difference the host alone has", []string{"alone"}, []string{"whole"}, ""},
		{"differences stage 4 and stage 5 own apart", []string{"facts"}, []string{"whole"}, ""},
		{"a difference stage 4 alone owns", nil, []string{"facts"}, ""},
		{"differences stage 5 owns, one of a rule both stages own", []string{"both"}, []string{"whole"},
			stagePackageWide},
	}
	differential := &buildBodyDifferential{}
	hostFindings := map[string][]check.Finding{}
	builtFindings := map[string][]check.Finding{}
	for index, each := range cases {
		file := fmt.Sprintf("/mixed/body%d.go", index)
		differential.bodies = append(differential.bodies, comparedBody{body: reachedBody{
			packageIdentifier: "example.com/mixed", file: file, line: 1, name: each.name}, firstLine: 1, lastLine: 9})
		for line, rule := range each.host {
			hostFindings[file] = append(hostFindings[file], check.Finding{File: file, Rule: rule, Line: line + 2,
				Column: 1, Message: "m"})
		}
		for line, rule := range each.built {
			builtFindings[file] = append(builtFindings[file], check.Finding{File: file, Rule: rule, Line: line + 5,
				Column: 1, Message: "m"})
		}
	}
	differential.compareFindings(hostFindings, builtFindings, stages,
		&findingCauses{filesDiffer: map[string]bool{"example.com/mixed": true}})
	for _, each := range cases {
		got := findingStage("")
		for stage, lines := range differential.classed {
			if slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, " "+each.name+":") }) {
				got = stage
			}
		}
		failed := slices.ContainsFunc(differential.failures, func(line string) bool {
			return strings.Contains(line, " "+each.name+":")
		})
		if got != each.want || failed != (each.want == "") {
			test.Errorf("%s: classed %q and failed %t, want classed %q", each.name, got, failed, each.want)
		}
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
	runOverConfigured(test, root, exitFindings, nil, "--no-fix", "--build-dependence")
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
	}, "--no-fix", "--build-dependence")

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
