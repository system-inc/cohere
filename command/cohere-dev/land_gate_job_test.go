//go:build unix

package main

import (
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// landGateJobFixture is a landing ready to gate as a land-gate job: a module with one package and one test, ahra stood
// in for by a script that answers the ask with landGateFixtureJob, the row with the job's end and the log with the
// job's output, and go stood in for by one that records each call, so a test can read what Kirk's Mac ran.
type landGateJobFixture struct {
	*landFixture
	// asks is every call the stand-in ahra took, one line each, command the last ask's command, whole, and at the
	// commit it named.
	asks, command, at string
	// goCalls is every go command Kirk's Mac ran, one line each.
	goCalls string
}

// landGateFixtureJob is the job id the stand-in ahra's ask answers with.
const landGateFixtureJob = "J-tst1234"

// newLandGateJobFixture makes the fixture, its stand-in ahra answering the row with an Ended job whose outcome is
// outcome and exit is exit, and the log with jobOutput, in which `COMMIT` is the commit the ask named.
func newLandGateJobFixture(test *testing.T, outcome string, exit int, jobOutput string) *landGateJobFixture {
	test.Helper()
	fixture := &landGateJobFixture{landFixture: newLandFixture(test)}
	stand := test.TempDir()
	fixture.asks = filepath.Join(stand, "asks")
	fixture.command = filepath.Join(stand, "command")
	fixture.goCalls = filepath.Join(stand, "go")
	recorder := "#!/bin/sh\necho \"$*\" >> '" + fixture.goCalls + "'\n"
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"),
		[]byte(recorder), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	log := filepath.Join(stand, "log")
	writeFixtureFile(test, log, jobOutput)
	fixture.at = filepath.Join(stand, "at")
	// The ask is `loom job <command> --kind ... --at cohere@<sha>`, the row `loom job <id> --json` and the log
	// `loom job <id> --log`, as ahra takes them. The row is Ended at once, so no read waits landGateReadEvery.
	row := `{"id":"` + landGateFixtureJob + `","state":"Ended","outcome":"` + outcome + `","outcomeWhy":null,` +
		`"exit":` + strconv.Itoa(exit) + `}`
	ahra := "#!/bin/sh\necho \"$*\" >> '" + fixture.asks + "'\n" +
		"if [ \"$4\" = --json ]; then echo '" + row + "'; exit 0; fi\n" +
		"if [ \"$4\" = --log ]; then sed \"s/COMMIT/$(cat '" + fixture.at + "')/\" '" + log + "'; exit 0; fi\n" +
		"printf '%s' \"$3\" > '" + fixture.command + "'\n" +
		"previous=''\nfor argument in \"$@\"; do\n" +
		"  [ \"$previous\" = --at ] && echo \"${argument#cohere@}\" > '" + fixture.at + "'\n" +
		"  previous=\"$argument\"\ndone\n" +
		"echo 'job: asked'\necho '" + landGateFixtureJob + " queued, 1st in line, 0 ahead'\n"
	standIn := filepath.Join(stand, "ahra")
	if writeError := executable_testing.WriteFile(standIn, []byte(ahra), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, gateVariable+"="+landGateKindSetting,
		loomStandInVariable+"="+standIn)
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
	writeFixtureFile(test, filepath.Join(worktree, "alpha", "alpha_test.go"),
		"package alpha\n\nimport \"testing\"\n\nfunc TestAlpha(test *testing.T) {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a module"))
	fixture.git(worktree, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
	return fixture
}

// landing lands the fixture's worktree, and returns whether main moved and everything the landing said.
func (fixture *landGateJobFixture) landing() (bool, string) {
	fixture.test.Helper()
	before := fixture.git(fixture.repository, "rev-parse", "main")
	landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested as a land-gate job")
	landing.Dir, landing.Env = fixture.worktree("a"), fixture.environment
	// Ignored: whether it landed is read from main, and what it said is the output.
	output, _ := landing.CombinedOutput()
	return fixture.git(fixture.repository, "rev-parse", "main") != before, string(output)
}

// A landing's tests as a land-gate job: a job that passed lands it, naming the job and the trust its box earns on the
// land line; a job whose test failed refuses it, naming the test; and a job that gave no verdict, Loom's or the
// machine's failure or a box that couldn't lay the tree, refuses it saying why, and never sends its tests to Kirk's
// Mac (#1v2shq7, item 2). Its end is read from its row's outcome, since ahra's wait verb is gone (#rr818c8).
func TestALandingTestedAsALandGateJobLandsOnlyOnItsVerdict(test *testing.T) {
	test.Parallel()
	passing := boxMarker + "COMMIT on linux/amd64\n" + `{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n"
	for _, testCase := range []struct {
		name      string
		outcome   string
		exit      int
		jobOutput string
		lands     bool
		said      []string
	}{
		{
			name:      "the job passes",
			outcome:   "Exit",
			jobOutput: "PASS lay: no submodule pinned\n" + passing,
			lands:     true,
			said:      []string{"asked Loom for land-gate job " + landGateFixtureJob, "the land lock stays held"},
		},
		{
			name:    "the job fails a test",
			outcome: "Exit",
			exit:    1,
			jobOutput: boxMarker + "COMMIT on linux/amd64\n" +
				`{"Action":"fail","Package":"example.com/fixture/alpha","Test":"TestPlantedFailure"}` + "\n" +
				`{"Action":"fail","Package":"example.com/fixture/alpha"}` + "\n",
			said: []string{"land-gate job " + landGateFixtureJob + "'s tests on linux/amd64 failed",
				"TestPlantedFailure"},
		},
		{
			name:      "the job meets infrastructure",
			outcome:   "Infrastructure",
			jobOutput: passing,
			said: []string{"no verdict", "never fall back to Kirk's Mac",
				"it met Loom's or the machine's failure, never the tree's (infrastructure)"},
		},
		{
			name:      "the box holds no lay kit",
			outcome:   "Exit",
			exit:      3,
			jobOutput: "FAIL lay: kit not on this box\n",
			said:      []string{"no verdict", "its box could not lay the tree (FAIL lay: kit not on this box)"},
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newLandGateJobFixture(test, testCase.outcome, testCase.exit, testCase.jobOutput)
			landed, output := fixture.landing()
			// Read before whether it landed, so a refusal that fell back to Kirk's Mac says so on its own.
			// Ignored: a Mac that ran no go command wrote no record.
			goCalls, _ := os.ReadFile(fixture.goCalls)
			for _, line := range strings.Split(string(goCalls), "\n") {
				if strings.HasPrefix(line, "test") {
					test.Errorf("Kirk's Mac ran a go test for a landing the job alone may test: go %s", line)
				}
			}
			if landed != testCase.lands {
				test.Fatalf("landed: %t, want %t:\n%s", landed, testCase.lands, output)
			}
			for _, want := range testCase.said {
				if !strings.Contains(output, want) {
					test.Errorf("the landing does not say %q:\n%s", want, output)
				}
			}
			if !testCase.lands {
				return
			}
			body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
			want := "go test of the module on a scratch box (linux/amd64, 1 packages, scratch box, shared uid; " +
				"land-gate job " + landGateFixtureJob + "), nothing it skipped"
			if !strings.Contains(body, want) {
				test.Errorf("the landing's body lacks %q:\n%s", want, body)
			}
		})
	}
}

// landGateEncoded is the encoded script in a job's command, from `bash -c "$(echo <it> | base64 -d)"`.
var landGateEncoded = regexp.MustCompile(`bash -c "\$\(echo ([A-Za-z0-9+/=]+) \| base64 -d\)" gate `)

// The job's command lays the commit's submodules with the lay kit at its pinned commit, refusing as `FAIL lay:` on a
// box without it, then runs sharedBoxGate, unchanged, against the pinned toolchain, its goroot left for the box's
// shell to expand. The ask names the kind, the landing's task and the commit the gate ran at, whose tree is the one
// that lands, and the job's end is read from its row.
func TestALandGateJobAsksForTheLayKitAndTheSharedBoxGate(test *testing.T) {
	test.Parallel()
	fixture := newLandGateJobFixture(test, "Exit", 0, boxMarker+"COMMIT on linux/amd64\n"+
		`{"Action":"pass","Package":"example.com/fixture/alpha"}`+"\n")
	landed, output := fixture.landing()
	if !landed {
		test.Fatalf("the landing did not land:\n%s", output)
	}
	command, readError := os.ReadFile(fixture.command)
	if readError != nil {
		test.Fatal(readError)
	}
	kit := `git -C ~/loom-suite.git cat-file -e fa30c7fada620692ef689df61b95a003e9c6327c:contract/proof/lay.sh || ` +
		`{ echo "FAIL lay: kit not on this box"; exit 3; }; ` +
		`bash <(git -C ~/loom-suite.git show fa30c7fada620692ef689df61b95a003e9c6327c:contract/proof/lay.sh) -- bash -c `
	if !strings.HasPrefix(string(command), kit) {
		test.Errorf("the job's command does not open with the lay kit's line:\n%s", command)
	}
	found := landGateEncoded.FindSubmatch(command)
	if found == nil {
		test.Fatalf("the job's command runs no encoded script through bash:\n%s", command)
	}
	script, decodeError := base64.StdEncoding.DecodeString(string(found[1]))
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	if !strings.HasPrefix(string(script), "set -euo pipefail\n") || !strings.HasSuffix(string(script), sharedBoxGate) {
		test.Errorf("the job's script is not sharedBoxGate under set -euo pipefail:\n%s", script)
	}
	arguments := ` gate "$HOME/adamic-tools/go" ` + sharedBoxToolchainDigest + ` '' ./...`
	if !strings.HasSuffix(string(command), arguments) {
		test.Errorf("the job's command does not end with %q:\n%s", arguments, command)
	}
	asks, readError := os.ReadFile(fixture.asks)
	if readError != nil {
		test.Fatal(readError)
	}
	at, readError := os.ReadFile(fixture.at)
	if readError != nil {
		test.Fatal(readError)
	}
	gated := strings.TrimSpace(string(at))
	if len(gated) != 40 || fixture.git(fixture.repository, "rev-parse", gated+"^{tree}") !=
		fixture.git(fixture.repository, "rev-parse", "main^{tree}") {
		test.Errorf("the job was asked at %q, not the full commit of the tree that landed", gated)
	}
	for _, want := range []string{
		" --kind land-gate --task " + fixtureTask + " --at cohere@" + gated + "\n",
		"loom job " + landGateFixtureJob + " --json\n",
		"loom job " + landGateFixtureJob + " --log\n",
	} {
		if !strings.Contains(string(asks), want) {
			test.Errorf("ahra was not asked %q:\n%s", want, asks)
		}
	}
}

// The land reads a land-gate job's row every landGateReadEvery until it has ended, and reads its end from the row: an
// Exit is the job's own code, every other outcome no verdict in its own words, a cancel naming who and why. A run of
// rows that didn't read gives up, one alone never does, and the wait gives up at landGateWithin, still in line.
func TestALandGateJobIsReadUntilItsRowSaysItEnded(test *testing.T) {
	test.Parallel()
	running := `{"id":"J-tst1234","state":"Running","outcome":null,"exit":null}`
	for _, row := range []struct {
		name string
		// rows are what each read answers in turn, the last again once they run out; empty is a read that failed.
		rows  []string
		code  int
		said  string
		reads int
	}{
		{"an exit after two reads in line", []string{running, running,
			`{"state":"Ended","outcome":"Exit","exit":4}`}, 4, "", 3},
		{"a read that failed between", []string{running, "", `{"state":"Ended","outcome":"Exit","exit":0}`}, 0, "",
			3},
		{"a cancel", []string{`{"state":"Ended","outcome":"Cancelled","outcomeWhy":"killed","exit":143,` +
			`"cancelledBy":"kirkouimet","cancelledReason":"a wrong ask"}`}, 0,
			"land-gate job J-tst1234 gave no verdict: it was cancelled by kirkouimet: a wrong ask", 1},
		{"infrastructure under its old name", []string{`{"state":"Ended","outcome":"Infra","outcomeWhy":"box lost"}`},
			0, "it met Loom's or the machine's failure, never the tree's (infrastructure): box lost", 1},
		{"an outcome unknown here", []string{`{"state":"Ended","outcome":"Unplaced"}`}, 0,
			"it ended Unplaced, an outcome this land doesn't know", 1},
		{"an Exit with no exit", []string{`{"state":"Ended","outcome":"Exit","exit":null}`}, 0,
			"it ended Exit with no exit", 1},
		{"rows that never read", []string{""}, 0, "the door failed to read it 5 times in a row", 5},
		{"a job still in line at the bound", []string{running}, 0, "the wait on it ran out after 2h0m0s", 241},
	} {
		reads := 0
		slept := time.Duration(0)
		code, said := waitForLandGateJob("J-tst1234", func() ([]byte, error) {
			answer := row.rows[min(reads, len(row.rows)-1)]
			reads++
			if answer == "" {
				return []byte("job: Zach didn't answer"), errors.New("exit status 1")
			}
			return []byte(answer), nil
		}, func(duration time.Duration) {
			if duration != landGateReadEvery {
				test.Errorf("%s: slept %s between reads, want %s", row.name, duration, landGateReadEvery)
			}
			slept += duration
		}, func() time.Duration { return slept })
		if code != row.code || !strings.Contains(said, row.said) || (row.said == "") != (said == "") ||
			reads != row.reads {
			test.Errorf("%s: read %d times to %d, %q; want %d times to %d, %q", row.name, reads, code, said, row.reads,
				row.code, row.said)
		}
	}
}
