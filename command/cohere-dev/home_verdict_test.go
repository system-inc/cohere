//go:build unix

package main

import (
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/needs"
)

// Home's run is read from its marker and its `go test -json`: the commit and platform it names, each package that
// passed or failed, each failed test, and each skipped test by its top-level name, a skipped subtest among them. What
// a gap prints around the command is not JSON and is passed over.
func TestHomesRunIsReadFromItsMarkerAndItsJson(test *testing.T) {
	test.Parallel()
	output := strings.Join([]string{
		"gap: Home is quiet, running",
		homeMarker + "0123456789abcdef on linux/amd64",
		`{"Action":"pass","Package":"example.com/one"}`,
		`{"Action":"skip","Package":"example.com/one","Test":"TestSkipsOnLinux"}`,
		`{"Action":"skip","Package":"example.com/one","Test":"TestParent/a subtest"}`,
		`{"Action":"skip","Package":"example.com/two","Test":"TestSkipsOnLinux"}`,
		// A package with no test files: a package-level skip, which is no test to run again.
		`{"Action":"skip","Package":"example.com/three"}`,
		`{"Action":"output","Package":"example.com/two","Test":"TestBroken","Output":"broken here\n"}`,
		`{"Action":"fail","Package":"example.com/two","Test":"TestBroken"}`,
		`{"Action":"fail","Package":"example.com/two"}`,
		"exit 1",
	}, "\n")
	verdict := readTestJson([]byte(output))
	if verdict.commit != "0123456789abcdef" || verdict.operatingSystem != "linux" || verdict.architecture != "amd64" {
		test.Errorf("the marker read as %q on %s/%s", verdict.commit, verdict.operatingSystem, verdict.architecture)
	}
	if verdict.packagesPassed != 1 || !reflect.DeepEqual(verdict.packagesFailed, []string{"example.com/two"}) ||
		!reflect.DeepEqual(verdict.failedTests, []string{"example.com/two TestBroken"}) || !verdict.failed() {
		test.Errorf("results read as %d passed, %v failed, tests %v", verdict.packagesPassed, verdict.packagesFailed,
			verdict.failedTests)
	}
	wantSkipped := map[string][]string{
		"example.com/one": {"TestSkipsOnLinux", "TestParent"},
		"example.com/two": {"TestSkipsOnLinux"},
	}
	if !reflect.DeepEqual(verdict.skipped, wantSkipped) {
		test.Errorf("skips read as %v, want %v", verdict.skipped, wantSkipped)
	}
	if got := verdict.failedOutput["example.com/two TestBroken"]; !reflect.DeepEqual(got, []string{"broken here\n"}) {
		test.Errorf("the failed test's output read as %q", got)
	}
}

// Only gap's own refusal for a held Home asks again: a gap that ran, with no error, is not held and says nothing
// (it printed "cohere-dev: <nil>" on every landing Home took), and neither is another refusal or another failure.
func TestOnlyAHeldHomeIsAskedAgain(test *testing.T) {
	test.Parallel()
	refused := exec.Command("sh", "-c", "exit 3").Run()
	failed := exec.Command("sh", "-c", "exit 1").Run()
	for _, row := range []struct {
		name     string
		runError error
		output   string
		held     bool
	}{
		{"a gap that ran", nil, "gap Home done", false},
		{"held by a hand job", refused, "gap Home refused · Home holds hand job 01M (mind, Running)", true},
		{"held by another gap", refused, "gap Home refused · Home is held by gap-123", true},
		{"refused for another reason", refused, "gap Home refused · HTTP 403 no mind LOOM_MINDS lists", false},
		{"a run that failed", failed, "Home holds hand job", false},
	} {
		if got := gapHeld(row.runError, []byte(row.output)); got != row.held {
			test.Errorf("%s read as held %t, want %t", row.name, got, row.held)
		}
	}
}

// The witness's window on Home is 09:00 up to 15:00 UTC, whatever the clock's zone.
func TestTheWitnessWindowIsHomesFromNineToThreeUtc(test *testing.T) {
	test.Parallel()
	mountain := time.FixedZone("MST", -7*60*60)
	for _, testCase := range []struct {
		at   time.Time
		want bool
	}{
		{time.Date(2026, 11, 9, 8, 59, 0, 0, time.UTC), false},
		{time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC), true},
		{time.Date(2026, 11, 9, 14, 59, 0, 0, time.UTC), true},
		{time.Date(2026, 11, 9, 15, 0, 0, 0, time.UTC), false},
		{time.Date(2026, 11, 9, 2, 0, 0, 0, mountain), true},
		{time.Date(2026, 11, 9, 12, 0, 0, 0, mountain), false},
	} {
		if got := inWitnessWindow(testCase.at); got != testCase.want {
			test.Errorf("at %s the window reads %t, want %t", testCase.at, got, testCase.want)
		}
	}
}

// Home is the default (#acdf5mc): unset or "on", a landing goes to Home, which inside the witness's window stops
// there before anything reaches her, and says so; "off" is the escape hatch, the Mac alone.
func TestALandingTestsOnHomeUnlessItSaysOff(test *testing.T) {
	test.Parallel()
	inside := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	for _, setting := range []string{"", "on"} {
		if _, unavailable := runHomeGate(
			"",
			"",
			nil,
			nil,
			inside,
			setting,
		); unavailable != "it is the witness's window on Home, 09:00 to 15:00 UTC" {
			test.Errorf("with %s=%q a landing did not go to Home: %q", homeGateVariable, setting, unavailable)
		}
	}
	if _, unavailable := runHomeGate("", "", nil, nil, inside, "off"); unavailable != "Home is off for this landing ("+
		homeGateVariable+"=off)" {
		test.Errorf("with %s=off a landing did not stay on the Mac and say why: %q", homeGateVariable, unavailable)
	}
}

// A test in a file Linux never compiles, by its name or by its build constraint, is found from the syntax, since
// Home cannot report a test that does not exist there; one Linux compiles, or only Linux does, is not.
func TestTheTestsLinuxNeverCompilesAreFoundFromTheSyntax(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	for name, contents := range map[string]string{
		"go.mod":                          "module example.com/fixture\n\ngo 1.27\n",
		"alpha/alpha_darwin_test.go":      "package alpha\n\nimport \"testing\"\n\nfunc TestByName(test *testing.T) {}\n",
		"alpha/tagged_test.go":            "//go:build darwin\n\npackage alpha\n\nimport \"testing\"\n\nfunc TestByTag(test *testing.T) {}\n",
		"alpha/everywhere_test.go":        "package alpha\n\nimport \"testing\"\n\nfunc TestEverywhere(test *testing.T) {}\n",
		"alpha/linux_only_test.go":        "//go:build linux\n\npackage alpha\n\nimport \"testing\"\n\nfunc TestLinux(test *testing.T) {}\n",
		"alpha/testdata/x_darwin_test.go": "package x\n\nimport \"testing\"\n\nfunc TestFixture(test *testing.T) {}\n",
	} {
		writeFixtureFile(test, filepath.Join(root, name), contents)
	}
	found, foundError := testsNeverCompiledOn(root, []project.Module{{Directory: ".", Path: "example.com/fixture"}},
		"darwin", "arm64", "linux", "amd64")
	if foundError != nil {
		test.Fatal(foundError)
	}
	// The walk reads a directory's files in name order, so alpha_darwin_test.go comes before tagged_test.go.
	want := map[string][]string{"example.com/fixture/alpha": {"TestByName", "TestByTag"}}
	if !reflect.DeepEqual(found, want) {
		test.Errorf("found %v, want %v", found, want)
	}
	// The other way round, so the lander's platform is read from its arguments wherever this runs: a Linux lander
	// against a darwin box finds the Linux-only test.
	found, foundError = testsNeverCompiledOn(root, []project.Module{{Directory: ".", Path: "example.com/fixture"}},
		"linux", "amd64", "darwin", "arm64")
	if foundError != nil {
		test.Fatal(foundError)
	}
	if want := map[string][]string{"example.com/fixture/alpha": {"TestLinux"}}; !reflect.DeepEqual(found, want) {
		test.Errorf("a Linux lander against darwin found %v, want %v", found, want)
	}
	// The workspace leaves a lone root module's path empty, so its import path is read from its go.mod: named by
	// the directory alone, go test can't load the package (command/cohere [setup failed], Nov 12).
	found, foundError = testsNeverCompiledOn(root, []project.Module{{Directory: "."}}, "darwin", "arm64", "linux",
		"amd64")
	if foundError != nil {
		test.Fatal(foundError)
	}
	if _, named := found["example.com/fixture/alpha"]; !named {
		test.Errorf("a root module with no recorded path named its packages %v, want by example.com/fixture", found)
	}
}

// Through a real landing with Home stood in for: a test Home skipped and a test in a file Linux never compiles both
// run on the Mac in the same landing, named on its land line; a failure on Home refuses the landing; and Home with no
// verdict, unreachable or answering about another tree, sends the whole test run to the Mac and says why (#acdf5mc).
// A tree holding a raceGated package has Home run it under -race after the module: a pass is named on the land line, a
// race refuses the landing, and a run that never reached its -race run is no verdict, which the land line names as no
// -race run (#bnabymg).
func TestALandingTestsOnHomeAndRunsHomesSkipsOnTheMac(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", "its fixture's darwin-only test runs in a landing's Mac half, which a lander "+
		"on any other platform never compiles")
	for _, testCase := range []struct {
		name    string
		home    func(commit string) string
		raced   bool
		lands   bool
		inBody  []string
		refusal []string
	}{
		{
			name: "Home passes, with a skip",
			home: func(commit string) string {
				return homeMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					`{"Action":"skip","Package":"example.com/fixture/alpha","Test":"TestPlantedLinuxSkip"}` + "\n"
			},
			lands: true,
			inBody: []string{"go test of the module on Home (linux/amd64, 1 packages), then 2 tests it skipped or " +
				"never compiled on the Mac (TestPlantedDarwinOnly, TestPlantedLinuxSkip)"},
		},
		{
			name: "Home fails a test",
			home: func(commit string) string {
				return homeMarker + commit + " on linux/amd64\n" +
					`{"Action":"fail","Package":"example.com/fixture/alpha","Test":"TestPlantedFailure"}` + "\n" +
					`{"Action":"fail","Package":"example.com/fixture/alpha"}` + "\n"
			},
			refusal: []string{"Home's tests on linux/amd64 failed", "TestPlantedFailure"},
		},
		{
			name: "Home passes, and its race run passes",
			home: func(commit string) string {
				return homeMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n" +
					homeRaceMarker + "\n" +
					`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n"
			},
			raced:  true,
			lands:  true,
			inBody: []string{"go test of the module on Home (linux/amd64, 2 packages), 1 of them again under -race"},
		},
		{
			name: "Home passes, and its race run finds a race",
			home: func(commit string) string {
				return homeMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					homeRaceMarker + "\n" +
					`{"Action":"output","Package":"example.com/fixture/internal/edit","Test":"TestPlantedRace",` +
					`"Output":"WARNING: DATA RACE\\n"}` + "\n" +
					`{"Action":"fail","Package":"example.com/fixture/internal/edit","Test":"TestPlantedRace"}` + "\n" +
					`{"Action":"fail","Package":"example.com/fixture/internal/edit"}` + "\n"
			},
			raced:   true,
			refusal: []string{"Home's -race run on linux/amd64 failed", "TestPlantedRace", "WARNING: DATA RACE"},
		},
		{
			name: "Home passes, and says nothing of its race run",
			home: func(commit string) string {
				return homeMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n"
			},
			raced: true,
			lands: true,
			inBody: []string{"go test of the module on the Mac, since Home's run said nothing of its -race run; " +
				"no -race run of its 1 packages"},
		},
		{
			name:  "Home unreachable",
			home:  func(string) string { return "ssh: connect to host Home: Connection refused\n" },
			lands: true,
			inBody: []string{"go test of the module on the Mac, since Home's run never said what it tested (ssh: " +
				"connect to host Home: Connection refused, exit status 255)"},
		},
		{
			name:   "Home answers about another tree",
			home:   func(string) string { return homeMarker + strings.Repeat("f", 40) + " on linux/amd64\n" },
			lands:  true,
			inBody: []string{"go test of the module on the Mac, since Home tested ffffffff"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandFixture(test)
			worktreeA := fixture.worktree("a")
			// A module whose one package has a test only the Mac compiles.
			writeFixtureFile(test, filepath.Join(worktreeA, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
			writeFixtureFile(test, filepath.Join(worktreeA, "alpha", "alpha_darwin_test.go"),
				"package alpha\n\nimport \"testing\"\n\nfunc TestPlantedDarwinOnly(test *testing.T) {}\n")
			if testCase.raced {
				// A package at one of raceGated's directories, so the gate asks Home for its -race run.
				writeFixtureFile(test, filepath.Join(worktreeA, "internal", "edit", "edit_test.go"),
					"package edit\n\nimport \"testing\"\n\nfunc TestPlantedRace(test *testing.T) {}\n")
			}
			fixture.git(worktreeA, "add", ".")
			fixture.git(worktreeA, "commit", "-q", "-m", fixtureSubject("a module"))
			fixture.git(worktreeA, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
			head := fixture.git(worktreeA, "rev-parse", "HEAD")
			// The stand-in answers about the commit the gate runs at, the branch merged with main, which it reads from
			// its first argument as Home's run would.
			standIn := filepath.Join(test.TempDir(), "home")
			script := "#!/bin/sh\ncommit=\"$1\"\n"
			reply := testCase.home("COMMIT")
			script += "cat <<'REPLY' | sed \"s/COMMIT/$commit/\"\n" + reply + "REPLY\n"
			if strings.Contains(testCase.name, "unreachable") {
				script += "exit 255\n"
			}
			if writeError := executable_testing.WriteFile(standIn, []byte(script), 0o755); writeError != nil {
				test.Fatal(writeError)
			}
			before := fixture.git(fixture.repository, "rev-parse", "main")
			landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested on Home")
			landing.Dir, landing.Env = worktreeA, append(fixture.environment, homeGateVariable+"="+standIn)
			output, landError := landing.CombinedOutput()
			after := fixture.git(fixture.repository, "rev-parse", "main")
			if landed := after != before; landed != testCase.lands {
				test.Fatalf("landed: %t, want %t (%v):\n%s", landed, testCase.lands, landError, output)
			}
			if !testCase.lands {
				for _, want := range testCase.refusal {
					if !strings.Contains(string(output), want) {
						test.Errorf("the refusal does not say %q:\n%s", want, output)
					}
				}
				return
			}
			body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
			for _, want := range testCase.inBody {
				if !strings.Contains(body, want) {
					test.Errorf("the landing's body lacks %q:\n%s\n(head %s)", want, body, head)
				}
			}
		})
	}
}
