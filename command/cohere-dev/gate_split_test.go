//go:build unix

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// newSplitFixture is a landing ready to split its gate across Sun2 and Marin: the mac box fixture's module with its
// -race package, internal/edit, and two packages more, beta and internal/go_engine, the Square's starting half, which
// is a race token too. Both boxes answer with a passing run of exactly what each is handed, 7.5s a package and 3.5s
// under -race, internal/go_engine in 3 shards of its 4 tests, as the box's script shards it (sharded_run.go).
func newSplitFixture(test *testing.T) *macBoxFixture {
	test.Helper()
	fixture := newMacBoxFixture(test, true)
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "beta", "beta_test.go"),
		"package beta\n\nimport \"testing\"\n\nfunc TestBeta(test *testing.T) {}\n")
	writeFixtureFile(test, filepath.Join(worktree, "internal", "go_engine", "engine_test.go"),
		"package go_engine\n\nimport \"testing\"\n\nfunc TestEngine(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("two packages more"))
	fixture.reply("Sun2.passes", "7.5 3.5\n")
	fixture.reply("Marin.passes", "7.5 3.5\n")
	for _, box := range []string{"Sun2", "Marin"} {
		fixture.reply(box+".shards", "example.com/fixture/internal/go_engine 3 TestEngine TestEngineAgain "+
			"TestEngineOnceMore TestEngineAtLast\n")
	}
	return fixture
}

// unreply takes away the reply files named, which the split fixture wrote.
func (fixture *macBoxFixture) unreply(files ...string) {
	fixture.test.Helper()
	for _, file := range files {
		if removeError := os.Remove(filepath.Join(fixture.replies, file)); removeError != nil {
			fixture.test.Fatal(removeError)
		}
	}
}

// timesPath is where the fixture's landing keeps the gate's times, in its private pool's directory.
func (fixture *macBoxFixture) timesPath() string {
	fixture.test.Helper()
	for _, variable := range slices.Backward(fixture.environment) {
		if home, found := strings.CutPrefix(variable, homeVariable+"="); found {
			return filepath.Join(userCacheDirectoryIn(home), "cohere", "test-slots", gateTimesFile)
		}
	}
	fixture.test.Fatal("the fixture names no pool home")
	return ""
}

// remoteArguments is the packages and the race tokens box's last remote was handed, read from what the stand-in kept.
func (fixture *macBoxFixture) remoteArguments(box string) (string, string) {
	fixture.test.Helper()
	remote := readMaybeMissing(fixture.test, filepath.Join(fixture.replies, box+".remote"))
	found := macBoxRemote.FindSubmatch(remote)
	if found == nil {
		fixture.test.Fatalf("%s's remote is not the encoded script and its arguments:\n%s", box, remote)
	}
	return string(found[6]), string(found[4])
}

// The fixture's race tokens, as racePatterns writes them for its tree.
var splitFixtureRaces = "./internal/edit,./internal/go_engine@" +
	"Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad"

// A landing with both gate boxes free splits its tests across them, both on the commit being landed (#bc8nf0r). With
// no times kept yet it is the Square's starting split, internal/go_engine alone on Sun2 and every other package and
// every race token on Marin, each handed as import paths; the halves' one verdict lands it, its land line naming which
// box ran which half, and its body names go_engine's shards, its time the slowest's; and the gate's times are written
// from both halves, plain and -race. A second landing, with times
// that make alpha the heaviest, still splits by the starting rule, since Elapsed is a binary's wall and not its CPU.
// Neither runs a go test on Kirk's Mac.
func TestASplitGateRunsHalfOnEachBoxAndLandsOnBothVerdicts(test *testing.T) {
	test.Parallel()
	fixture := newSplitFixture(test)
	landed, output, calls, goCalls := fixture.landing(gateSplitVariable + "=on")
	if !landed {
		test.Fatalf("the split landing did not land:\n%s", output)
	}
	for _, line := range strings.Split(goCalls, "\n") {
		if strings.HasPrefix(line, "test") {
			test.Errorf("Kirk's Mac ran a go test: %s", line)
		}
	}
	if !slices.ContainsFunc(strings.Split(goCalls, "\n"), func(line string) bool {
		return strings.HasPrefix(line, "list -e ")
	}) {
		test.Errorf("the gate never listed its packages here:\n%s", goCalls)
	}
	for _, box := range []string{"Sun2", "Marin"} {
		if !strings.Contains(calls, "loom gap --box "+box+" --within 1h -- ssh "+box+" ") {
			test.Errorf("%s ran no half:\n%s", box, calls)
		}
	}
	packages, races := fixture.remoteArguments("Sun2")
	if packages != "example.com/fixture/internal/go_engine" || races != "" {
		test.Errorf("Sun2 was handed %q and races %q, want internal/go_engine alone", packages, races)
	}
	packages, races = fixture.remoteArguments("Marin")
	if packages != "example.com/fixture/alpha example.com/fixture/beta example.com/fixture/internal/edit" ||
		races != splitFixtureRaces {
		test.Errorf("Marin was handed %q and races %q, want every other package and every race token", packages,
			races)
	}
	body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
	for _, want := range []string{
		"go test of the module on Sun2 and Marin (darwin/arm64, 4 packages: Sun2 1, Marin 3 and 2 -race tokens, " +
			"scratch box, shared uid), 2 of them again under -race, nothing it skipped",
		"its gate Sun2 slowest test binaries example.com/fixture/internal/go_engine 7.5s (slowest of 3 shards); " +
			"Marin slowest test binaries example.com/fixture/alpha 7.5s",
		"its -race run Marin slowest test binaries example.com/fixture/internal/edit 3.5s",
	} {
		if !strings.Contains(body, want) {
			test.Errorf("the landing's body lacks %q:\n%s", want, body)
		}
	}
	contents, readError := os.ReadFile(fixture.timesPath())
	if readError != nil {
		test.Fatalf("the split gate wrote no times: %v", readError)
	}
	times := gateTimes{}
	if unmarshalError := json.Unmarshal(contents, &times); unmarshalError != nil {
		test.Fatal(unmarshalError)
	}
	want := gateTimes{
		Plain: map[string]float64{"example.com/fixture/alpha": 7.5, "example.com/fixture/beta": 7.5,
			"example.com/fixture/internal/edit": 7.5, "example.com/fixture/internal/go_engine": 7.5},
		Race: map[string]float64{"example.com/fixture/internal/edit": 3.5,
			"example.com/fixture/internal/go_engine": 3.5},
	}
	if !reflect.DeepEqual(times, want) {
		test.Errorf("the gate's times read %+v, want %+v", times, want)
	}

	// With times that make alpha the heaviest, the next landing still gives internal/go_engine a half alone.
	written, marshalError := json.Marshal(gateTimes{Plain: map[string]float64{"example.com/fixture/alpha": 900}})
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	writeFixtureFile(test, fixture.timesPath(), string(written))
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "beta", "more_test.go"),
		"package beta\n\nimport \"testing\"\n\nfunc TestMore(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a test more"))
	landed, output, _, _ = fixture.landing(gateSplitVariable + "=on")
	if !landed {
		test.Fatalf("the landing with times did not land:\n%s", output)
	}
	if packages, races := fixture.remoteArguments("Sun2"); packages != "example.com/fixture/internal/go_engine" ||
		races != "" {
		test.Errorf("with times, Sun2 was handed %q and races %q, want internal/go_engine alone", packages, races)
	}
}

// A split gate refuses its landing whenever its halves aren't one green gate: a half that fails a test, naming its box
// and the test; a half whose report omits a package it was given, or names one it wasn't, naming the package; and a
// half's -race run that ends before its tokens. Each is driven through the real landing, both boxes at once.
func TestASplitGateRefusesUnlessBothHalvesAreGreenAndWhole(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name    string
		replies map[string]string
		// unreplied are reply files the split fixture wrote that the case takes away.
		unreplied []string
		said      []string
		unsaid    []string
	}{
		{
			name:    "Marin fails a test",
			replies: map[string]string{"Marin.fails": "example.com/fixture/beta"},
			said: []string{"Marin's tests on darwin/arm64 failed, so nothing landed",
				"example.com/fixture/beta TestPlantedFailure"},
		},
		{
			// A red half's report may fall short too, and its failing test is what the refusal names.
			name: "Marin fails a test and her report omits a package",
			replies: map[string]string{"Marin.fails": "example.com/fixture/beta",
				"Marin.omit": "example.com/fixture/alpha"},
			said: []string{"Marin's tests on darwin/arm64 failed, so nothing landed",
				"example.com/fixture/beta TestPlantedFailure"},
			unsaid: []string{"didn't cover its packages"},
		},
		{
			name:    "Marin's report omits a package she was given",
			replies: map[string]string{"Marin.omit": "example.com/fixture/beta"},
			said: []string{"the split gate's halves didn't cover its packages exactly once each, so its verdict is " +
				"none: Marin's run never reported example.com/fixture/beta, which it was given",
				"example.com/fixture/beta, which the gate lists, was reported 0 times across both halves"},
		},
		{
			name:    "Sun2's report names a package she wasn't given",
			replies: map[string]string{"Sun2.extra": "example.com/fixture/planted"},
			said: []string{"Sun2's run reported example.com/fixture/planted, which it wasn't given",
				"example.com/fixture/planted was reported, which the gate doesn't list"},
		},
		{
			name:    "a shard of Sun2's never ends a test it was dealt",
			replies: map[string]string{"Sun2.shardomit": "TestEngineOnceMore"},
			said: []string{"the split gate's halves didn't cover its packages exactly once each, so its verdict is " +
				"none: Sun2's shards: TestEngineOnceMore (example.com/fixture/internal/go_engine) ended 0 times " +
				"across its shards"},
		},
		{
			name: "Marin's run says nothing of her -race run",
			replies: map[string]string{"Marin.run": boxMarker + "COMMIT on darwin/arm64\n" +
				`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n"},
			unreplied: []string{"Marin.passes"},
			said: []string{"no verdict from a gate box, so nothing landed",
				"Marin's run said nothing of its -race run"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newSplitFixture(test)
			for file, contents := range testCase.replies {
				fixture.reply(file, contents)
			}
			fixture.unreply(testCase.unreplied...)
			landed, output, _, _ := fixture.landing(gateSplitVariable + "=on")
			if landed {
				test.Fatalf("the landing landed:\n%s", output)
			}
			for _, want := range testCase.said {
				if !strings.Contains(output, want) {
					test.Errorf("the refusal does not say %q:\n%s", want, output)
				}
			}
			for _, unwanted := range testCase.unsaid {
				if strings.Contains(output, unwanted) {
					test.Errorf("the refusal says %q:\n%s", unwanted, output)
				}
			}
		})
	}
}

// With one gate box held, a split waits for her a bounded while, asking again, then runs today's whole gate on the
// free box, the land line saying why; with both held, the whole gate's own wait applies; and with the split off, the
// whole gate runs on the first box as before the split, Marin never asked.
func TestASplitGateFallsBackToTheWholeGateNamingWhy(test *testing.T) {
	test.Parallel()
	whole := boxMarker + "COMMIT on darwin/arm64\n" +
		`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
		boxRaceMarker + "\n" +
		`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n"
	for _, testCase := range []struct {
		name        string
		replies     map[string]string
		unreplied   []string
		environment []string
		lands       bool
		said        []string
		inBody      []string
		// whole is the box whose remote is handed every package the gate lists, and unasked prefixes of calls never
		// taken.
		whole      string
		unasked    []string
		claimReads map[string]int
	}{
		{
			name:        "Marin held",
			replies:     map[string]string{"Marin.held": "", "Sun2.run": whole},
			unreplied:   []string{"Sun2.passes"},
			environment: []string{gateSplitVariable + "=on", gateSplitWaitVariable + "=1ms"},
			lands:       true,
			said:        []string{"Sun2 is free and Marin held; the split waits for a second box holding the lock"},
			inBody: []string{"go test of the module on Sun2 (darwin/arm64, 1 packages, scratch box, shared uid)",
				"; Marin stayed held ", "s, so the whole gate ran on Sun2"},
			whole:      "Sun2",
			unasked:    []string{"loom gap --box Marin --within"},
			claimReads: map[string]int{"Marin": 2},
		},
		{
			name:        "both held",
			replies:     map[string]string{"Sun2.held": "", "Marin.held": ""},
			environment: []string{gateSplitVariable + "=on", gateWaitVariable + "=1ms"},
			said: []string{"every gate box is held (Sun2, Marin); the land waits in line holding the lock",
				"every gate box stayed held through "},
			unasked:    []string{"loom gap --box Sun2 --within", "loom gap --box Marin --within"},
			claimReads: map[string]int{"Sun2": 3, "Marin": 3},
		},
		{
			name:        "the split off",
			replies:     map[string]string{"Sun2.run": whole},
			unreplied:   []string{"Sun2.passes", "Marin.passes"},
			environment: []string{gateSplitVariable + "=" + gateSplitOffSetting},
			lands:       true,
			inBody: []string{"go test of the module on Sun2 (darwin/arm64, 1 packages, scratch box, shared uid), 1 " +
				"of them again under -race, nothing it skipped"},
			whole:   "Sun2",
			unasked: []string{"loom gap --box Marin"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newSplitFixture(test)
			for file, contents := range testCase.replies {
				fixture.reply(file, contents)
			}
			fixture.unreply(testCase.unreplied...)
			landed, output, calls, _ := fixture.landing(testCase.environment...)
			if landed != testCase.lands {
				test.Fatalf("landed: %t, want %t:\n%s", landed, testCase.lands, output)
			}
			for _, want := range testCase.said {
				if !strings.Contains(output, want) {
					test.Errorf("the landing does not say %q:\n%s", want, output)
				}
			}
			lines := strings.Split(calls, "\n")
			for _, unwanted := range testCase.unasked {
				for _, line := range lines {
					if strings.HasPrefix(line, unwanted) {
						test.Errorf("ahra was asked %q, which this landing never asks", line)
					}
				}
			}
			for box, least := range testCase.claimReads {
				reads := 0
				for _, line := range lines {
					if line == "loom gap --box "+box {
						reads++
					}
				}
				if reads < least {
					test.Errorf("%s's claim was read %d times, want at least %d:\n%s", box, reads, least, calls)
				}
			}
			if testCase.whole != "" {
				packages, races := fixture.remoteArguments(testCase.whole)
				if packages != "example.com/fixture/alpha example.com/fixture/beta example.com/fixture/internal/edit "+
					"example.com/fixture/internal/go_engine" || races != splitFixtureRaces {
					test.Errorf("%s was handed %q and races %q, want every package the gate lists and every token",
						testCase.whole, packages, races)
				}
			}
			if !testCase.lands {
				return
			}
			body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
			for _, want := range testCase.inBody {
				if !strings.Contains(body, want) {
					test.Errorf("the landing's body lacks %q:\n%s", want, body)
				}
			}
		})
	}
}

// The split is the Square's: with internal/go_engine listed, its plain run alone on one half and everything else on
// the other, times or none, since Elapsed is a binary's wall and not its CPU; with no package there, heaviest first
// onto the lighter half, anything untimed weighing a second; and never a half with no package, which would test the
// directory its go test runs in.
func TestTheGatesHalvesSplitByTheirLastTimes(test *testing.T) {
	test.Parallel()
	starting := gateListing{
		packages:        []string{"m/a", "m/b", "m/c", "m/internal/go_engine"},
		racePackages:    map[string]string{"./a": "m/a", "./internal/go_engine@Concurrent": "m/internal/go_engine"},
		startingPackage: "m/internal/go_engine",
	}
	startingRaces := []string{"./a", "./internal/go_engine@Concurrent"}
	// A listing with no package at internal/go_engine, which the halves split greedily.
	greedy := gateListing{
		packages:     []string{"m/a", "m/b", "m/c", "m/d"},
		racePackages: map[string]string{"./a": "m/a", "./d@Concurrent": "m/d"},
	}
	greedyRaces := []string{"./a", "./d@Concurrent"}
	for _, row := range []struct {
		name    string
		listing gateListing
		races   []string
		times   *gateTimes
		want    [2]gateHalf
	}{
		{"no times", starting, startingRaces, nil, [2]gateHalf{
			{packages: []string{"m/internal/go_engine"}, races: []string{}},
			{packages: []string{"m/a", "m/b", "m/c"}, races: startingRaces},
		}},
		{"times, and internal/go_engine listed", starting, startingRaces, &gateTimes{
			Plain: map[string]float64{"m/a": 100, "m/b": 10, "m/c": 10, "m/internal/go_engine": 5},
			Race:  map[string]float64{"m/a": 50},
		}, [2]gateHalf{
			{packages: []string{"m/internal/go_engine"}, races: []string{}},
			{packages: []string{"m/a", "m/b", "m/c"}, races: startingRaces},
		}},
		{"no times and no starting package", greedy, greedyRaces, nil, [2]gateHalf{
			{packages: []string{"m/a", "m/c"}, races: []string{"./a"}},
			{packages: []string{"m/b", "m/d"}, races: []string{"./d@Concurrent"}},
		}},
		{"times and no starting package", greedy, greedyRaces, &gateTimes{
			Plain: map[string]float64{"m/a": 100, "m/b": 10, "m/c": 10, "m/d": 5},
			Race:  map[string]float64{"m/a": 50},
		}, [2]gateHalf{
			{packages: []string{"m/a"}, races: []string{}},
			{packages: []string{"m/b", "m/c", "m/d"}, races: greedyRaces},
		}},
		// The heavy race token fills the first half, every package goes to the second, and the first takes the
		// second's lightest, the last it was given.
		{"a half the race tokens filled", greedy, greedyRaces, &gateTimes{Race: map[string]float64{"m/a": 100}},
			[2]gateHalf{
				{packages: []string{"m/d"}, races: []string{"./a"}},
				{packages: []string{"m/a", "m/b", "m/c"}, races: []string{"./d@Concurrent"}},
			}},
	} {
		halves := splitGate(row.listing, row.races, row.times)
		for index := range halves {
			if halves[index].races == nil {
				halves[index].races = []string{}
			}
		}
		if !reflect.DeepEqual(halves, row.want) {
			test.Errorf("%s split as %+v, want %+v", row.name, halves, row.want)
		}
	}
}

// Coverage is proven package by package: each half's report holds exactly the packages it was given, its -race run
// its tokens' packages, and across both halves every listed package and every token appears once. A package with no
// test files counts as reported by its package-level skip.
func TestASplitGatesCoverageIsProvenPackageByPackage(test *testing.T) {
	test.Parallel()
	listing := gateListing{
		packages:     []string{"m/a", "m/b", "m/empty"},
		racePackages: map[string]string{"./a": "m/a"},
	}
	races := []string{"./a"}
	halves := [2]gateHalf{{box: "Sun2", packages: []string{"m/a"}},
		{box: "Marin", packages: []string{"m/b", "m/empty"}, races: races}}
	run := func(events ...string) string {
		return boxMarker + "c on darwin/arm64\n" + strings.Join(events, "\n") + "\n"
	}
	pass := func(importPath string) string { return `{"Action":"pass","Package":"` + importPath + `"}` }
	skip := `{"Action":"skip","Package":"m/empty"}`
	raced := boxRaceMarker + "\n" + pass("m/a")
	// read is a half's verdict, its -race run read apart after the marker, as readBoxRun reads it.
	read := func(output string) boxVerdict {
		moduleOutput, raceOutput, hasRace := strings.Cut(output, "\n"+boxRaceMarker+"\n")
		verdict := readTestJson([]byte(moduleOutput))
		if hasRace {
			race := readTestJson([]byte(raceOutput))
			verdict.race = &race
		}
		return verdict
	}
	for _, row := range []struct {
		name   string
		sun2   string
		marin  string
		halves [2]gateHalf
		gaps   []string
	}{
		{"whole", run(pass("m/a")), run(pass("m/b"), skip, raced), halves, nil},
		{"a package omitted", run(pass("m/a")), run(pass("m/b"), raced), halves, []string{
			"Marin's run never reported m/empty, which it was given",
			"m/empty, which the gate lists, was reported 0 times across both halves"}},
		{"a package doubled", run(pass("m/a"), pass("m/a")), run(pass("m/b"), skip, raced), halves, []string{
			"Sun2's run reported m/a 2 times",
			"m/a, which the gate lists, was reported 2 times across both halves"}},
		{"a -race package omitted", run(pass("m/a")), run(pass("m/b"), skip, boxRaceMarker+"\n"+pass("m/b")), halves,
			[]string{"Marin's -race run never reported m/a, which it was given",
				"Marin's -race run reported m/b, which it wasn't given"}},
		{"a package given to neither half", run(pass("m/a")), run(pass("m/b"), raced), [2]gateHalf{halves[0],
			{box: "Marin", packages: []string{"m/b"}, races: races}}, []string{
			"m/empty, which the gate lists, was reported 0 times across both halves"}},
		{"a race token given to both halves", run(pass("m/a"), raced), run(pass("m/b"), skip, raced), [2]gateHalf{
			{box: "Sun2", packages: []string{"m/a"}, races: races}, halves[1]}, []string{
			"the race token ./a went to 2 halves"}},
	} {
		gaps := splitCoverageGaps(listing, races, row.halves, [2]boxVerdict{read(row.sun2), read(row.marin)})
		if !slices.Equal(gaps, row.gaps) {
			test.Errorf("%s: the gaps read\n%q\nwant\n%q", row.name, gaps, row.gaps)
		}
	}
}

// The split's setting is on unless it says off, and refused when it says anything else.
func TestTheGatesSplitIsOnUnlessItSaysOff(test *testing.T) {
	test.Parallel()
	for setting, want := range map[string]bool{"": true, "on": true, " on ": true, "off": false} {
		if splitting, settingError := gateSplitSetting(setting); splitting != want || settingError != nil {
			test.Errorf("%s=%q read as %t, %v; want %t", gateSplitVariable, setting, splitting, settingError, want)
		}
	}
	if _, settingError := gateSplitSetting("half"); settingError == nil ||
		!strings.Contains(settingError.Error(), "is neither on nor off") {
		test.Errorf("%s=half read as %v, want refused", gateSplitVariable, settingError)
	}
}
