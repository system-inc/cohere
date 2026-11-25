//go:build unix

package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// newSplitFixture is a landing ready to split its gate across Sun2 and Marin: the mac box fixture's module with its
// -race package, internal/edit, and two packages more, beta and internal/go_language, the Square's starting half, which
// is a race token too. Both boxes answer with a passing run of exactly what each is handed, 7.5s a package and 3.5s
// under -race, internal/go_language in 3 shards of its 4 tests, as the box's script shards it (sharded_run.go).
func newSplitFixture(test *testing.T) *macBoxFixture {
	test.Helper()
	fixture := newMacBoxFixture(test, true)
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "beta", "beta_test.go"),
		"package beta\n\nimport \"testing\"\n\nfunc TestBeta(test *testing.T) {}\n")
	writeFixtureFile(test, filepath.Join(worktree, "internal", "go_language", "main_test.go"),
		"package go_language\n\nimport \"testing\"\n\nfunc TestChecker(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("two packages more"))
	fixture.reply("Sun2.passes", "7.5 3.5\n")
	fixture.reply("Marin.passes", "7.5 3.5\n")
	for _, box := range []string{"Sun2", "Marin"} {
		fixture.reply(box+".shards", "example.com/fixture/internal/go_language 3 TestChecker TestCheckerAgain "+
			"TestCheckerOnceMore TestCheckerAtLast\n")
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
var splitFixtureRaces = "./internal/edit,./internal/go_language@" +
	"Concurrent|AtOnce|InParallel|Simultaneous|SideBySide|BesideTheLoad"

// A landing with both gate boxes free splits its tests across them, both on the commit being landed (#bc8nf0r). With
// no times kept yet it is the Square's starting split, internal/go_language alone on Sun2 and every other package and
// every race token on Marin, each handed as import paths; the halves' one verdict lands it, its land line naming which
// box ran which half and the wall each half's steps measured, and its body names go_language's shards, its time the
// slowest's; and the gate's times are written from both halves, plain and -race, with each box's half recorded. A
// second landing, with times that make alpha the heaviest, splits by them (#q1mvjgg): alpha alone on Sun2 and the rest
// on Marin, each half's estimate said beside its measured wall. Neither runs a go test on Kirk's Mac.
func TestASplitGateRunsHalfOnEachBoxAndLandsOnBothVerdicts(test *testing.T) {
	test.Parallel()
	fixture := newSplitFixture(test)
	fixture.reply("Sun2.steps", "95 0\n")
	fixture.reply("Marin.steps", "700 300\n")
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
	if packages != "example.com/fixture/internal/go_language" || races != "" {
		test.Errorf("Sun2 was handed %q and races %q, want internal/go_language alone", packages, races)
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
		"; its halves by the starting split, with no times kept: Sun2 ran 1m35s; Marin ran 16m40s; ",
		"its gate Sun2 by step tests 1m35s; slowest test binaries example.com/fixture/internal/go_language 7.5s " +
			"(slowest of 3 shards); Marin by step tests 11m40s; slowest test binaries example.com/fixture/alpha 7.5s",
		"its -race run Marin by step race 5m0s; slowest test binaries example.com/fixture/internal/edit 3.5s",
	} {
		if !strings.Contains(body, want) {
			test.Errorf("the landing's body lacks %q:\n%s", want, body)
		}
	}
	if want := "Sun2 and Marin's gate, its halves by the starting split"; !strings.Contains(output, want) {
		test.Errorf("the land line does not say %q:\n%s", want, output)
	}
	// Sun2's half was the sharded package alone, so her record holds no item seconds and gives no factor; Marin's
	// holds alpha, beta and internal/edit plain and both race packages.
	readTimes := func() gateTimes {
		test.Helper()
		contents, readError := os.ReadFile(fixture.timesPath())
		if readError != nil {
			test.Fatalf("the split gate wrote no times: %v", readError)
		}
		times := gateTimes{}
		if unmarshalError := json.Unmarshal(contents, &times); unmarshalError != nil {
			test.Fatal(unmarshalError)
		}
		return times
	}
	want := gateTimes{
		Plain: map[string]float64{"example.com/fixture/alpha": 7.5, "example.com/fixture/beta": 7.5,
			"example.com/fixture/internal/edit": 7.5, "example.com/fixture/internal/go_language": 7.5},
		Race: map[string]float64{"example.com/fixture/internal/edit": 3.5,
			"example.com/fixture/internal/go_language": 3.5},
		Halves: map[string]gateHalfRecord{"Sun2": {Sharded: 7.5, Wall: 95}, "Marin": {Weight: 29.5, Wall: 1000}},
	}
	if times := readTimes(); !reflect.DeepEqual(times, want) {
		test.Errorf("the gate's times read %+v, want %+v", times, want)
	}

	// With times that make alpha the heaviest, and no half recorded, each box's factor is the default: alpha's 900s
	// reckon Sun2 at 7m30s, and the rest, go_language's 7.5s at full value, Marin at 18.5s.
	written, marshalError := json.Marshal(gateTimes{
		Plain: map[string]float64{"example.com/fixture/alpha": 900, "example.com/fixture/beta": 7.5,
			"example.com/fixture/internal/edit": 7.5, "example.com/fixture/internal/go_language": 7.5},
		Race: map[string]float64{"example.com/fixture/internal/edit": 3.5,
			"example.com/fixture/internal/go_language": 3.5},
	})
	if marshalError != nil {
		test.Fatal(marshalError)
	}
	writeFixtureFile(test, fixture.timesPath(), string(written))
	fixture.reply("Sun2.steps", "440 0\n")
	fixture.reply("Marin.steps", "12 6\n")
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "beta", "more_test.go"),
		"package beta\n\nimport \"testing\"\n\nfunc TestMore(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a test more"))
	landed, output, _, _ = fixture.landing(gateSplitVariable + "=on")
	if !landed {
		test.Fatalf("the landing with times did not land:\n%s", output)
	}
	if packages, races := fixture.remoteArguments("Sun2"); packages != "example.com/fixture/alpha" || races != "" {
		test.Errorf("with times, Sun2 was handed %q and races %q, want alpha alone", packages, races)
	}
	if packages, races := fixture.remoteArguments("Marin"); packages != "example.com/fixture/beta "+
		"example.com/fixture/internal/edit example.com/fixture/internal/go_language" || races != splitFixtureRaces {
		test.Errorf("with times, Marin was handed %q and races %q, want every other package and every race token",
			packages, races)
	}
	for _, said := range []string{"testing ", "'s half on Sun2 in her gap, 1 packages, reckoned at 7m30s; ",
		"Sun2 and Marin's gate, its halves by their recorded times: Sun2 est 7m30s, ran 7m20s; Marin est 19s, " +
			"ran 18s"} {
		if !strings.Contains(output, said) {
			test.Errorf("the landing does not say %q:\n%s", said, output)
		}
	}
	body = fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
	if want := "; its halves by their recorded times: Sun2 est 7m30s, ran 7m20s; Marin est 19s, ran 18s; "; !strings.
		Contains(body, want) {
		test.Errorf("the landing's body lacks %q:\n%s", want, body)
	}
	// Each half's record is this gate's: Sun2's alpha, and Marin's beta, internal/edit and both race packages, with
	// go_language's slowest shard apart.
	if halves := readTimes().Halves; !reflect.DeepEqual(halves, map[string]gateHalfRecord{
		"Sun2": {Weight: 7.5, Wall: 440}, "Marin": {Weight: 22, Sharded: 7.5, Wall: 18},
	}) {
		test.Errorf("the gate's halves read %+v", halves)
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
			replies: map[string]string{"Sun2.shardomit": "TestCheckerOnceMore"},
			said: []string{"the split gate's halves didn't cover its packages exactly once each, so its verdict is " +
				"none: Sun2's shards: TestCheckerOnceMore (example.com/fixture/internal/go_language) ended 0 times " +
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
					"example.com/fixture/internal/go_language" || races != splitFixtureRaces {
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

// The split is by recorded time (#q1mvjgg). With no times it is the Square's starting split, internal/go_language's
// plain run alone on one half, or with no package there each item weighing a second. With times, every package and
// token goes, heaviest first, to the half whose estimate is lower once it holds it, each weight scaled by the box's
// factor but internal/go_language's, sharded on all of a box's cores and so at full value; and never a half with no
// package, which would test the directory its go test runs in.
func TestTheGatesHalvesBalanceByTheirRecordedTimes(test *testing.T) {
	test.Parallel()
	starting := gateListing{
		packages:       []string{"m/a", "m/b", "m/c", "m/internal/go_language"},
		racePackages:   map[string]string{"./a": "m/a", "./internal/go_language@Concurrent": "m/internal/go_language"},
		shardedPackage: "m/internal/go_language",
	}
	startingRaces := []string{"./a", "./internal/go_language@Concurrent"}
	// A listing with no package at internal/go_language.
	greedy := gateListing{
		packages:     []string{"m/a", "m/b", "m/c", "m/d"},
		racePackages: map[string]string{"./a": "m/a", "./d@Concurrent": "m/d"},
	}
	greedyRaces := []string{"./a", "./d@Concurrent"}
	greedyTimes := gateTimes{
		Plain: map[string]float64{"m/a": 100, "m/b": 10, "m/c": 10, "m/d": 5},
		Race:  map[string]float64{"m/a": 50},
	}
	// reckoned is a half placed by times, holding packages and races, reckoned at seconds.
	reckoned := func(box string, packages []string, races []string, seconds float64) gateHalf {
		return gateHalf{box: box, packages: packages, races: races, weighed: true,
			estimate: time.Duration(seconds * float64(time.Second))}
	}
	for _, row := range []struct {
		name    string
		listing gateListing
		races   []string
		times   *gateTimes
		want    [2]gateHalf
	}{
		{"no times", starting, startingRaces, nil, [2]gateHalf{
			{box: "Sun2", packages: []string{"m/internal/go_language"}, races: []string{}},
			{box: "Marin", packages: []string{"m/a", "m/b", "m/c"}, races: startingRaces},
		}},
		// m/a's 100s, scaled by the default factor, go to Sun2, and the two race tokens, the second weighing the
		// median of the recorded ones, to Marin; internal/go_language's 5s count whole.
		{"times, and internal/go_language listed", starting, startingRaces, &gateTimes{
			Plain: map[string]float64{"m/a": 100, "m/b": 10, "m/c": 10, "m/internal/go_language": 5},
			Race:  map[string]float64{"m/a": 50},
		}, [2]gateHalf{
			reckoned("Sun2", []string{"m/a", "m/b", "m/internal/go_language"}, []string{}, 60),
			reckoned("Marin", []string{"m/c"}, startingRaces, 55),
		}},
		{"no times and no starting package", greedy, greedyRaces, nil, [2]gateHalf{
			{box: "Sun2", packages: []string{"m/a", "m/c"}, races: []string{"./a"}},
			{box: "Marin", packages: []string{"m/b", "m/d"}, races: []string{"./d@Concurrent"}},
		}},
		{"times and no starting package", greedy, greedyRaces, &greedyTimes, [2]gateHalf{
			reckoned("Sun2", []string{"m/a", "m/b", "m/d"}, []string{}, 57.5),
			reckoned("Marin", []string{"m/c"}, greedyRaces, 55),
		}},
		// The heavy race token fills Sun2, every package goes to Marin, and Sun2 takes Marin's lightest, the last
		// she was given.
		{"a half the race tokens filled", greedy, []string{"./a"}, &gateTimes{Race: map[string]float64{"m/a": 100}},
			[2]gateHalf{
				reckoned("Sun2", []string{"m/d"}, []string{"./a"}, 50.5),
				reckoned("Marin", []string{"m/a", "m/b", "m/c"}, []string{}, 1.5),
			}},
	} {
		halves := splitGate(row.listing, row.races, row.times, [2]string{"Sun2", "Marin"})
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

// A box's recorded half calibrates her factor, her wall less the sharded package's seconds over her item seconds, and
// moves the next split: the same times split otherwise once Sun2 is recorded three times as slow as Marin. With no
// record a box takes the other's factor, and with neither the default.
func TestARecordedHalfCalibratesItsBoxAndMovesTheNextSplit(test *testing.T) {
	test.Parallel()
	boxes := [2]string{"Sun2", "Marin"}
	for _, row := range []struct {
		name   string
		halves map[string]gateHalfRecord
		want   [2]float64
	}{
		{"none recorded", nil, [2]float64{gateSplitDefaultFactor, gateSplitDefaultFactor}},
		{"Marin recorded", map[string]gateHalfRecord{"Marin": {Weight: 2760, Wall: 1380}}, [2]float64{0.5, 0.5}},
		{"Sun2 recorded with the sharded package", map[string]gateHalfRecord{
			"Sun2": {Weight: 100, Sharded: 88, Wall: 238}}, [2]float64{1.5, 1.5}},
		// A half that held only the sharded package has no item seconds to scale, so it gives no factor.
		{"Sun2's half the sharded package alone", map[string]gateHalfRecord{"Sun2": {Sharded: 88, Wall: 95},
			"Marin": {Weight: 100, Wall: 40}}, [2]float64{0.4, 0.4}},
		{"both recorded", map[string]gateHalfRecord{"Sun2": {Weight: 100, Wall: 150},
			"Marin": {Weight: 100, Wall: 50}}, [2]float64{1.5, 0.5}},
	} {
		if factors := boxFactors(boxes, gateTimes{Halves: row.halves}); factors != row.want {
			test.Errorf("%s: the factors read %v, want %v", row.name, factors, row.want)
		}
	}
	listing := gateListing{
		packages:     []string{"m/a", "m/b", "m/c", "m/d"},
		racePackages: map[string]string{"./a": "m/a", "./d@Concurrent": "m/d"},
	}
	races := []string{"./a", "./d@Concurrent"}
	times := gateTimes{
		Plain: map[string]float64{"m/a": 100, "m/b": 10, "m/c": 10, "m/d": 5},
		Race:  map[string]float64{"m/a": 50},
	}
	before := splitGate(listing, races, &times, boxes)
	times.Halves = map[string]gateHalfRecord{"Sun2": {Weight: 100, Wall: 150}, "Marin": {Weight: 100, Wall: 50}}
	after := splitGate(listing, races, &times, boxes)
	if reflect.DeepEqual(before[0].packages, after[0].packages) {
		test.Errorf("Sun2's record moved nothing: %+v", after)
	}
	// m/a's 100s cost Sun2 150s and Marin 50s, so Marin takes it, and Sun2 the lighter items.
	want := [2]gateHalf{
		{box: "Sun2", packages: []string{"m/d"}, races: []string{"./a"}, weighed: true,
			estimate: 82500 * time.Millisecond},
		{box: "Marin", packages: []string{"m/a", "m/b", "m/c"}, races: []string{"./d@Concurrent"}, weighed: true,
			estimate: 85 * time.Second},
	}
	if !reflect.DeepEqual(after, want) {
		test.Errorf("the calibrated split is %+v, want %+v", after, want)
	}
}

// A package or race token with no recorded time weighs the median of the recorded ones of its kind, so a new package
// lands like an ordinary one; with none of its kind recorded it weighs gateSplitUnweighed. Only the sharded package
// counts at full value.
func TestAnUntimedItemWeighsTheMedianOfItsKind(test *testing.T) {
	test.Parallel()
	listing := gateListing{
		packages:       []string{"m/a", "m/b", "m/c", "m/new", "m/internal/go_language"},
		racePackages:   map[string]string{"./a": "m/a", "./b": "m/b", "./new": "m/new"},
		shardedPackage: "m/internal/go_language",
	}
	items := gateItems(listing, []string{"./a", "./b", "./new"}, gateTimes{
		Plain: map[string]float64{"m/a": 10, "m/b": 2, "m/c": 4, "m/internal/go_language": 90, "m/gone": 1000},
		Race:  map[string]float64{"m/a": 30, "m/b": 10},
	})
	want := []gateItem{
		{name: "m/a", weight: 10}, {name: "m/b", weight: 2}, {name: "m/c", weight: 4},
		// The median of 2, 4, 10 and 90; m/gone, which the listing no longer holds, is not among them.
		{name: "m/new", weight: 7}, {name: "m/internal/go_language", weight: 90, full: true},
		{name: "./a", race: true, weight: 30}, {name: "./b", race: true, weight: 10},
		{name: "./new", race: true, weight: 20},
	}
	if !slices.Equal(items, want) {
		test.Errorf("the items weigh %+v, want %+v", items, want)
	}
	if weight := weightOf(nil, "m/new", nil); weight != gateSplitUnweighed {
		test.Errorf("an item of a kind with no time weighs %g, want %g", weight, gateSplitUnweighed)
	}
	if weight := weightOf(nil, "m/new", []float64{5, 1, 3}); weight != 3 {
		test.Errorf("the median of 5, 1 and 3 weighs %g, want 3", weight)
	}
}

// The balancer on the gate's own shape, its times modeled on the live file of Nov 25: 437 packages, command/cohere's
// 350s and the rest of the heavy ones, most under a second, and 20 race tokens led by go_language/check's 444s and
// go_language/load's 335s; Marin's half recorded at 1,290s of wall for 2,760s of item seconds, Sun2's the sharded
// package alone, which gives no factor. Both halves reckon within 2% of each other, each holds two of the six heaviest
// items at least and one of the two heaviest race tokens, and every package and token is in exactly one half.
func TestTheBalancerSplitsTheGatesOwnShapeEvenly(test *testing.T) {
	test.Parallel()
	plain := map[string]float64{
		"command/cohere":                 350,
		"command/cohere-dev":             185.1,
		"internal/go_language/check":     118.2,
		"internal/go_language/load":      90.5,
		"internal/go_language":           87.9,
		"internal/release/cross_compile": 22.5,
		"internal/release/dispatch":      17.9,
		"internal/typescript_language/ecmascript/high_level_intermediate_representation": 16.4,
		"internal/typescript_language/ecmascript/regexp":                                 15.9,
		"internal/typescript_language/registry":                                          12.6,
		"internal/testing/guard":                                                         11.2,
		"internal/typescript_language/rules/typescript":                                  10.5,
		"internal/go_language/rename/global_rename":                                      10.5,
		"internal/text_rules/abbreviation":                                               10.1,
		"internal/typescript_language/program":                                           9.6,
	}
	// The other 422 packages, 0.2s to 1.82s each, about 426s in all.
	for index := range 422 {
		plain[fmt.Sprintf("internal/small/package_%03d", index)] = 0.2 + float64(index%10)*0.18
	}
	race := map[string]float64{
		"internal/go_language/check":                       443.7,
		"internal/go_language/load":                        334.5,
		"command/cohere-dev":                               179.6,
		"internal/go_language":                             166,
		"command/cohere":                                   93.4,
		"internal/typescript_language/program":             63.6,
		"internal/go_language/fact_resolver":               33.8,
		"internal/typescript_language/rules/typescript":    20.6,
		"internal/go_language/build_reads":                 18.6,
		"internal/go_language/build_dependence":            18.5,
		"internal/release/dispatch":                        18.5,
		"internal/go_language/cache":                       12.5,
		"internal/go_language/intermediate_representation": 10.6,
		"internal/edit":                                    3.8,
		"internal/go_language/check/checker":               3.4,
		"internal/go_language/bounded":                     3.2,
		"internal/go_language/rules/marker":                2.8,
		"internal/testing/executable_testing":              2.2,
		"internal/edit/write_root":                         1.1,
		"internal/go_language/suppression":                 1.1,
	}
	listing := gateListing{racePackages: map[string]string{}, shardedPackage: "m/internal/go_language"}
	times := gateTimes{Plain: map[string]float64{}, Race: map[string]float64{},
		Halves: map[string]gateHalfRecord{"Sun2": {Sharded: 88, Wall: 95}, "Marin": {Weight: 2760, Wall: 1290}}}
	for _, directory := range slices.Sorted(maps.Keys(plain)) {
		listing.packages = append(listing.packages, "m/"+directory)
		times.Plain["m/"+directory] = plain[directory]
	}
	races := []string{}
	for _, directory := range slices.Sorted(maps.Keys(race)) {
		token := "./" + directory
		if directory == "internal/go_language" {
			token += "@Concurrent|AtOnce"
		}
		races = append(races, token)
		listing.racePackages[token] = "m/" + directory
		times.Race["m/"+directory] = race[directory]
	}
	halves := splitGate(listing, races, &times, [2]string{"Sun2", "Marin"})
	estimates := [2]float64{halves[0].estimate.Seconds(), halves[1].estimate.Seconds()}
	if difference := math.Abs(estimates[0]-estimates[1]) / max(estimates[0], estimates[1]); difference > 0.02 {
		test.Errorf("the halves reckon %v, %.1f%% apart", estimates, difference*100)
	}
	heaviest := []string{"m/command/cohere", "./internal/go_language/check", "./internal/go_language/load",
		"m/command/cohere-dev", "./command/cohere-dev", "./internal/go_language@Concurrent|AtOnce"}
	for _, half := range halves {
		held := 0
		for _, item := range heaviest {
			if slices.Contains(half.packages, item) || slices.Contains(half.races, item) {
				held++
			}
		}
		if held < 2 {
			test.Errorf("%s holds %d of the six heaviest items: %+v", half.box, held, halves)
		}
		if !slices.Contains(half.races, "./internal/go_language/check") &&
			!slices.Contains(half.races, "./internal/go_language/load") {
			test.Errorf("%s holds neither of the two heaviest race tokens: %v", half.box, half.races)
		}
	}
	if gaps := splitCoverageGaps(listing, races, halves, [2]boxVerdict{
		{packagesReported: halves[0].packages}, {packagesReported: halves[1].packages}}); len(gaps) > 0 {
		test.Errorf("the halves don't cover the gate exactly once: %s", strings.Join(gaps, "; "))
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
