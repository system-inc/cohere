//go:build unix

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/needs"
)

// A box's run is read from its marker and its `go test -json`: the commit and platform it names, each package that
// passed or failed, each failed test, and each skipped test by its whole name, a skipped subtest among them, since
// nothing runs it again. What a gap prints around the command is not JSON and is passed over.
func TestABoxsRunIsReadFromItsMarkerAndItsJson(test *testing.T) {
	test.Parallel()
	output := strings.Join([]string{
		"gap Sun2 claimed, running",
		boxMarker + "0123456789abcdef on darwin/arm64",
		`{"Action":"pass","Package":"example.com/one"}`,
		`{"Action":"skip","Package":"example.com/one","Test":"TestSkipsOnDarwin"}`,
		`{"Action":"skip","Package":"example.com/one","Test":"TestParent/a subtest"}`,
		`{"Action":"skip","Package":"example.com/two","Test":"TestSkipsOnDarwin"}`,
		// A package with no test files: a package-level skip, which skipped no test.
		`{"Action":"skip","Package":"example.com/three"}`,
		`{"Action":"output","Package":"example.com/two","Test":"TestBroken","Output":"broken here\n"}`,
		`{"Action":"fail","Package":"example.com/two","Test":"TestBroken"}`,
		`{"Action":"fail","Package":"example.com/two"}`,
		"exit 1",
	}, "\n")
	verdict := readTestJson([]byte(output))
	if verdict.commit != "0123456789abcdef" || verdict.operatingSystem != "darwin" || verdict.architecture != "arm64" {
		test.Errorf("the marker read as %q on %s/%s", verdict.commit, verdict.operatingSystem, verdict.architecture)
	}
	if verdict.packagesPassed != 1 || !reflect.DeepEqual(verdict.packagesFailed, []string{"example.com/two"}) ||
		!reflect.DeepEqual(verdict.failedTests, []string{"example.com/two TestBroken"}) || !verdict.failed() {
		test.Errorf("results read as %d passed, %v failed, tests %v", verdict.packagesPassed, verdict.packagesFailed,
			verdict.failedTests)
	}
	wantSkipped := map[string][]string{
		"example.com/one": {"TestSkipsOnDarwin", "TestParent/a subtest"},
		"example.com/two": {"TestSkipsOnDarwin"},
	}
	if !reflect.DeepEqual(verdict.skipped, wantSkipped) {
		test.Errorf("skips read as %v, want %v", verdict.skipped, wantSkipped)
	}
	if got := verdict.failedOutput["example.com/two TestBroken"]; !reflect.DeepEqual(got, []string{"broken here\n"}) {
		test.Errorf("the failed test's output read as %q", got)
	}
}

// Only gap's own refusal for a held box passes it over for the next: a Mac whose claim outlasted gap's wait on it, a
// box another hand's gap holds, or one a hand job holds. A gap that ran, with no error, is not held (it printed
// "cohere-dev: <nil>" on every landing Home took), and neither is another refusal or another failure, which are the
// box's word on the landing.
func TestOnlyAHeldBoxIsPassedOver(test *testing.T) {
	test.Parallel()
	refused := exec.Command("sh", "-c", "exit 3").Run()
	failed := exec.Command("sh", "-c", "exit 1").Run()
	for _, row := range []struct {
		name     string
		runError error
		output   string
		held     bool
	}{
		{"a gap that ran", nil, "gap Sun2 done", false},
		{"a Mac's claim held past the wait", refused, "gap Sun2 refused · Sun2 stayed held by system_loom for 125 " +
			"minutes; cohere's land gate didn't run", true},
		{"held by a hand job", refused, "gap Sun2 refused · Sun2 holds hand job 01M (mind, Running)", true},
		{"held by another gap", refused, "gap Sun2 refused · Sun2 is held by gap-123", true},
		{"refused for another reason", refused, "gap Sun2 refused · HTTP 403 no mind LOOM_MINDS lists", false},
		{"a run that failed", failed, "Sun2 is held by gap-123", false},
	} {
		if got := gapHeld(row.runError, []byte(row.output)); got != row.held {
			test.Errorf("%s read as held %t, want %t", row.name, got, row.held)
		}
	}
}

// A test in a file Linux never compiles, by its name or by its build constraint, is found from the syntax, since a
// box on Linux cannot report a test that does not exist there; one Linux compiles, or only Linux does, is not.
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

// Through a real landing with the road stood in for by a command, as the land-gate kind's Linux box answers: what the
// box skipped and what Linux never compiles are named on the land line as run nowhere, never run on Kirk's Mac; a
// box's opt-in skips are named as opt-in; a race on the box refuses the landing with its reports; and a box with no
// verdict, unreachable, answering about another tree or silent on its -race run, refuses the landing and never sends a
// test to Kirk's Mac. With the gate off, Kirk's Mac runs the module and its -race run, which finds a real race.
func TestALandingReadsItsBoxsVerdictAndNeverRunsItsTestsOnKirksMac(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", "its fixture's darwin-only test is one the box's Linux never compiles only when "+
		"the lander is darwin")
	for _, testCase := range []struct {
		name string
		box  func(commit string) string
		// off lands with the gate off, on Kirk's Mac alone, with the real go.
		off   bool
		raced bool
		// racy makes the -race package's test a real race, for a -race run on Kirk's Mac to find.
		racy    bool
		lands   bool
		inBody  []string
		refusal []string
	}{
		{
			// An opt-in skip is no gate's test, named as opt-in and never as run nowhere, and an opt-in subtest beside
			// a passing one too.
			name: "the box passes, with a skip and opt-in skips",
			box: func(commit string) string {
				return boxMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					`{"Action":"output","Package":"example.com/fixture/alpha","Test":"TestPlantedCensus",` +
					`"Output":"    census_test.go:9: ` + needs.OptInPhrase + `COHERE_CENSUS set: a census\n"}` + "\n" +
					`{"Action":"skip","Package":"example.com/fixture/alpha","Test":"TestPlantedCensus"}` + "\n" +
					`{"Action":"output","Package":"example.com/fixture/alpha","Test":"TestPlantedRows/measured",` +
					`"Output":"    rows_test.go:9: ` + needs.OptInPhrase + `COHERE_ROWS set: a measure\n"}` + "\n" +
					`{"Action":"skip","Package":"example.com/fixture/alpha","Test":"TestPlantedRows/measured"}` + "\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha","Test":"TestPlantedRows/checked"}` + "\n" +
					`{"Action":"skip","Package":"example.com/fixture/alpha","Test":"TestPlantedLinuxSkip"}` + "\n"
			},
			lands: true,
			inBody: []string{"go test of the module on the stand-in (linux/amd64, 1 packages, scratch box, shared " +
				"uid), 1 never compiled on linux/amd64 and run nowhere: TestPlantedDarwinOnly " +
				"(example.com/fixture/alpha), 1 skipped on the stand-in and run nowhere: TestPlantedLinuxSkip " +
				"(example.com/fixture/alpha); opt-in, which no gate runs: TestPlantedCensus " +
				"(example.com/fixture/alpha), TestPlantedRows/measured (example.com/fixture/alpha)"},
		},
		{
			name: "the box passes, and its race run passes",
			box: func(commit string) string {
				return boxMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n" +
					boxRaceMarker + "\n" +
					`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n"
			},
			raced: true,
			lands: true,
			inBody: []string{"go test of the module on the stand-in (linux/amd64, 2 packages, scratch box, shared " +
				"uid), 1 of them again under -race"},
		},
		{
			name: "the box passes, and its race run finds a race",
			box: func(commit string) string {
				return boxMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
					boxRaceMarker + "\n" +
					// test2json gives the report to the test it was framing, here one that passed, as on a box.
					raceReportEvent("==================") + raceReportEvent("WARNING: DATA RACE") +
					raceReportEvent("Write at 0x00c000012345 by goroutine 8:") +
					raceReportEvent("  example.com/fixture/internal/edit.plantedWriter()") +
					raceReportEvent("==================") +
					`{"Action":"pass","Package":"example.com/fixture/internal/edit","Test":"TestFramedWhenPrinted"}` +
					"\n" +
					`{"Action":"fail","Package":"example.com/fixture/internal/edit","Test":"TestPlantedRace"}` + "\n" +
					`{"Action":"fail","Package":"example.com/fixture/internal/edit"}` + "\n"
			},
			raced: true,
			refusal: []string{"the stand-in's -race run on linux/amd64 failed", "TestPlantedRace", "1 races reported",
				"WARNING: DATA RACE", "edit.plantedWriter()"},
		},
		{
			name: "the box passes, and says nothing of its race run",
			box: func(commit string) string {
				return boxMarker + commit + " on linux/amd64\n" +
					`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n"
			},
			raced:   true,
			refusal: []string{"never fall back to Kirk's Mac", "the stand-in's run said nothing of its -race run"},
		},
		{
			name: "the box is unreachable",
			box:  func(string) string { return "ssh: connect to host sun2: Connection refused\n" },
			refusal: []string{"never fall back to Kirk's Mac", "the stand-in's run never said what it tested (ssh: " +
				"connect to host sun2: Connection refused, exit status 255)"},
		},
		{
			name:    "the box answers about another tree",
			box:     func(string) string { return boxMarker + strings.Repeat("f", 40) + " on linux/amd64\n" },
			refusal: []string{"never fall back to Kirk's Mac", "the stand-in tested ffffffff"},
		},
		{
			name:  "the gate off, and Kirk's Mac's -race run finds a race",
			off:   true,
			raced: true,
			racy:  true,
			refusal: []string{"COHERE_DEV_GATE=off, so the tests run here on Kirk's Mac",
				"its 1 -race packages run here too", "WARNING: DATA RACE",
				"the -race run here failed, so nothing landed"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandFixture(test)
			asked := filepath.Join(test.TempDir(), "asked")
			if testCase.racy {
				// The real go, since the race is the race detector's to find: the stand-in's directory comes off the
				// front of PATH.
				fixture.environment = append(fixture.environment, "PATH="+os.Getenv("PATH"))
			} else {
				recorder := "#!/bin/sh\necho \"$*\" >> '" + asked + "'\n"
				if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"),
					[]byte(recorder), 0o755); writeError != nil {
					test.Fatal(writeError)
				}
			}
			worktreeA := fixture.worktree("a")
			// A module whose one package has a test only a darwin lander compiles.
			writeFixtureFile(test, filepath.Join(worktreeA, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
			writeFixtureFile(test, filepath.Join(worktreeA, "alpha", "alpha_darwin_test.go"),
				"package alpha\n\nimport \"testing\"\n\nfunc TestPlantedDarwinOnly(test *testing.T) {}\n")
			if testCase.raced {
				// A package at one of raceGated's directories, so the gate asks the box for its -race run.
				body := "package edit\n\nimport \"testing\"\n\nfunc TestPlantedRace(test *testing.T) {}\n"
				if testCase.racy {
					// Two goroutines write one variable with nothing ordering them, which -race reports.
					body = "package edit\n\nimport (\n\t\"sync\"\n\t\"testing\"\n)\n\n" +
						"func TestPlantedRace(test *testing.T) {\n\tshared := 0\n\tvar group sync.WaitGroup\n" +
						"\tfor range 2 {\n\t\tgroup.Go(func() { shared++ })\n\t}\n\tgroup.Wait()\n" +
						"\ttest.Log(shared)\n}\n"
				}
				writeFixtureFile(test, filepath.Join(worktreeA, "internal", "edit", "edit_test.go"), body)
			}
			fixture.git(worktreeA, "add", ".")
			fixture.git(worktreeA, "commit", "-q", "-m", fixtureSubject("a module"))
			fixture.git(worktreeA, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
			setting := gateOffSetting
			if !testCase.off {
				// The stand-in answers about the commit the gate runs at, the branch merged with main, which it reads
				// from its first argument as the box's run would.
				setting = filepath.Join(test.TempDir(), "box")
				script := "#!/bin/sh\ncommit=\"$1\"\n"
				script += "cat <<'REPLY' | sed \"s/COMMIT/$commit/\"\n" + testCase.box("COMMIT") + "REPLY\n"
				if strings.Contains(testCase.name, "unreachable") {
					script += "exit 255\n"
				}
				if writeError := executable_testing.WriteFile(setting, []byte(script), 0o755); writeError != nil {
					test.Fatal(writeError)
				}
			}
			before := fixture.git(fixture.repository, "rev-parse", "main")
			landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested on a box")
			landing.Dir, landing.Env = worktreeA, append(fixture.environment, gateVariable+"="+setting)
			output, landError := landing.CombinedOutput()
			after := fixture.git(fixture.repository, "rev-parse", "main")
			if !testCase.racy {
				for _, line := range strings.Split(string(readMaybeMissing(test, asked)), "\n") {
					if strings.HasPrefix(line, "test") {
						test.Errorf("Kirk's Mac ran a go test for a landing a box tested: go %s", line)
					}
				}
			}
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
					test.Errorf("the landing's body lacks %q:\n%s", want, body)
				}
			}
		})
	}
}

// raceReportEvent is one line of a race report as test2json gives it, to a test that passed, as a JSON line.
func raceReportEvent(line string) string {
	return `{"Action":"output","Package":"example.com/fixture/internal/edit","Test":"TestFramedWhenPrinted",` +
		`"Output":"` + line + `\n"}` + "\n"
}
