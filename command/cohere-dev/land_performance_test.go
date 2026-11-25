//go:build unix

package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// performanceFixture is a landing of a tree with a command/cohere, so there is a cohere to measure: the land fixture's
// worktree a with a command/cohere of its own, cohere on the tree stood in for by a clean run, and ahra by macBoxAhra,
// Shackler answering from replies as a gate box does.
type performanceFixture struct {
	*landFixture
	replies string
}

// performanceCleanSummary is what the stand-in for cohere on the tree prints: a clean run, so the gate passes.
const performanceCleanSummary = `{"kind":"summary","schemaVersion":1,"label":".","verdict":"pass","findings":0,` +
	`"typeErrors":0,"gaps":{"programFiles":0,"crashedFiles":0,"formattingNotChecked":false,"crashes":[]}}`

func newPerformanceFixture(test *testing.T) *performanceFixture {
	test.Helper()
	fixture := &performanceFixture{landFixture: newLandFixture(test), replies: test.TempDir()}
	stand := test.TempDir()
	ahra := filepath.Join(stand, "ahra")
	if writeError := executable_testing.WriteFile(ahra, []byte(macBoxAhra), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	cohere := filepath.Join(stand, "cohere")
	if writeError := executable_testing.WriteFile(cohere, []byte("#!/bin/sh\necho '"+performanceCleanSummary+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, loomStandInVariable+"="+ahra, "GATE_REPLIES="+fixture.replies,
		gateCohereVariable+"="+cohere)
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "command", "cohere", "main.go"), "package main\n\nfunc main() {}\n")
	fixture.git(worktree, "add", "command")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a cohere to measure"))
	return fixture
}

// landing lands the fixture's worktree a with environment added, and returns whether main moved, everything the landing
// said and every call the stand-in ahra took.
func (fixture *performanceFixture) landing(environment ...string) (bool, string, string) {
	fixture.test.Helper()
	before := fixture.git(fixture.repository, "rev-parse", "main")
	landing := exec.Command(fixture.wrapper, "land", "--message", "a cohere measured")
	landing.Dir, landing.Env = fixture.worktree("a"), append(fixture.environment, environment...)
	// Ignored: whether it landed is read from main, and what it said is the output.
	output, _ := landing.CombinedOutput()
	landed := fixture.git(fixture.repository, "rev-parse", "main") != before
	return landed, string(output), string(readMaybeMissing(fixture.test, filepath.Join(fixture.replies, "calls")))
}

// performanceMainBinary is the digest of the binary main's file before the landing measured, another program's.
const performanceMainBinary = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"

// performanceFixtureBinary is the digest the stand-in Shackler says her binary has.
const performanceFixtureBinary = "5f0c3a9e1d2b4c6a8e0f1a3b5c7d9e1f2a4b6c8d0e2f4a6b8c0d2e4f6a8b0c2d"

// shacklerSays is everything the stand-in Shackler prints for a measurement, `COMMIT` the commit the remote names:
// a gap's own quiet line among the script's, her machine, the cold rounds at the root with walls coldWalls, deeafbf4's,
// which find 7 things there and exit 1, as an older tree may, and the warm and timeline rows, each with its fills.
// Each run's user is four times its wall and its system time a tenth of it, and each walks 120 files at the root and
// 95 at deeafbf4, its summary counting the findings it printed.
func shacklerSays(coldWalls []int64) string {
	var said strings.Builder
	say := func(format string, arguments ...any) {
		fmt.Fprintf(&said, performanceMarker+format+"\n", arguments...)
	}
	run := func(mode string, tree string, round string, wall int64, exit int, findings int) {
		filesWalked := 120
		if tree == performanceReferenceTree {
			filesWalked = 95
		}
		say("run %s %s %s %d %d %d %d %d %d %d", mode, tree, round, wall, 4*wall, wall/10, exit, findings,
			filesWalked, findings)
	}
	said.WriteString("quiet: load 0.89 0.89 1.28\n")
	say("phase mirror 312")
	say("phase lay 4108")
	say("phase toolchain 2950")
	say("phase modules 640")
	say("phase build 11870")
	say("tested COMMIT on darwin/arm64")
	say("binary %s", performanceFixtureBinary)
	say("machine 12 Apple M2 Max")
	say("go go1.27.1")
	say("load before 0.89 0.91 1.28")
	say("top before 175 _windowserver 15.0 WindowServer")
	run("cold", "root", "fill", 15000, 0, 0)
	run("cold", "root", "fill", 10100, 0, 0)
	for round, wall := range coldWalls {
		run("cold", "root", fmt.Sprint(round+1), wall, 0, 0)
	}
	say("phase cold-root 54800")
	run("cold", performanceReferenceTree, "fill", 4600, 1, 7)
	run("cold", performanceReferenceTree, "fill", 4300, 1, 7)
	run("cold", performanceReferenceTree, "1", 4200, 1, 7)
	run("cold", performanceReferenceTree, "2", 4000, 1, 7)
	say("phase cold-deeafbf4 17100")
	run("warm", "root", "fill", 9000, 0, 0)
	for round := 1; round <= performanceWarmRounds; round++ {
		run("warm", "root", fmt.Sprint(round), 800, 0, 0)
	}
	say("phase warm-root 9800")
	run("timeline", "root", "1", 9900, 0, 0)
	say("phase timeline-root 9900")
	say(`timeline {"kind":"timeline","stamps":[{"label":"main.begin","wallMicroseconds":12,` +
		`"userMicroseconds":0,"systemMicroseconds":0},{"label":"host.listing.end","wallMicroseconds":812345,` +
		`"userMicroseconds":0,"systemMicroseconds":0},{"build":"windows/amd64","label":"builds.end",` +
		`"wallMicroseconds":1000,"userMicroseconds":0,"systemMicroseconds":0},{"label":"handover.end",` +
		`"wallMicroseconds":5100499,"userMicroseconds":0,"systemMicroseconds":0},{"label":"builds.end",` +
		`"wallMicroseconds":6200500,"userMicroseconds":0,"systemMicroseconds":0},{"label":"run.end",` +
		`"wallMicroseconds":9700000,"userMicroseconds":0,"systemMicroseconds":0}]}`)
	say("load after 1.10 0.95 1.27")
	return said.String()
}

// performanceSpreadOf is a spread from its median, least and most.
func performanceSpreadOf(median int64, least int64, most int64) performanceSpread {
	return performanceSpread{Median: median, Minimum: least, Maximum: most}
}

// previousPerformance is a Performance.jsonc main holds before the landing, measured at commit on the machine named,
// its cold root row's wall and user given and every other row the stand-in's own, so only that row can move.
func previousPerformance(test *testing.T, commit string, machine string, wall performanceSpread,
	user performanceSpread) []byte {
	test.Helper()
	measurement, problem := readPerformance([]byte(strings.ReplaceAll(shacklerSays([]int64{9700, 9800, 9900}),
		"COMMIT", commit)), commit, nil)
	if problem != "" {
		test.Fatal(problem)
	}
	file := performanceFileOf(measurement, commit, time.Unix(0, 0))
	file.Machine.Name = machine
	if machine != performanceBox {
		file.Machine.Cpu, file.Machine.Cores = "Apple M1", 8
	}
	file.Checks.Cold.Root.Wall, file.Checks.Cold.Root.User = wall, user
	// main's binary was another program than the one being landed, as a landing that changes cohere builds.
	file.Tools.Binary = performanceMainBinary
	file.Note = "the first measurement, so nothing to compare"
	contents, renderError := renderPerformance(file)
	if renderError != nil {
		test.Fatal(renderError)
	}
	return contents
}

// sameBinaryPerformance is a previous file's contents with its binary the one the stand-in Shackler says she built,
// as main's file reads before a landing that changed nothing cohere compiles.
func sameBinaryPerformance(test *testing.T, contents []byte) []byte {
	test.Helper()
	file := performanceFile{}
	if decodeError := json.Unmarshal([]byte(performanceBody(string(contents))), &file); decodeError != nil {
		test.Fatal(decodeError)
	}
	file.Tools.Binary = performanceFixtureBinary
	same, renderError := renderPerformance(file)
	if renderError != nil {
		test.Fatal(renderError)
	}
	return same
}

// uncountedPerformance is a previous file's contents as main's files read before each row recorded what it checked.
func uncountedPerformance(test *testing.T, contents []byte) []byte {
	test.Helper()
	file := performanceFile{}
	if decodeError := json.Unmarshal([]byte(performanceBody(string(contents))), &file); decodeError != nil {
		test.Fatal(decodeError)
	}
	file.Checks.Cold.Root.Checked, file.Checks.Cold.Deeafbf4.Checked, file.Checks.Warm.Root.Checked = nil, nil, nil
	uncounted, renderError := renderPerformance(file)
	if renderError != nil {
		test.Fatal(renderError)
	}
	if strings.Contains(string(uncounted), `"checked"`) {
		test.Fatalf("a file with no checked counts still records some:\n%s", uncounted)
	}
	return uncounted
}

// performancePreviousCommit is the commit a previous file names, as main's file before the landing.
const performancePreviousCommit = "1111111111111111111111111111111111111111"

// A landing measured (#6wnsnq2): the push puts the gated commit in Loom's feed, Shackler's gap runs the script on it,
// and the landing commit's tree is the gated tree with Performance.jsonc and nothing else changed. The file opens with
// its header, names the gated commit, and holds the stand-in's numbers as medians with their ranges, her load before
// and after, that she was held quiet, the build, the timeline's areas and what each row checked. Its note reads against
// main's file before it: none is the first measurement, overlapping ranges read level, disjoint ones moved, another
// machine is a line of its own, never compared, and a file with no checked counts says so and compares its times. The
// land line and the landing's body say the note.
func TestALandingIsMeasuredOnShacklerAndLandsItsNumbers(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name     string
		previous []byte
		note     string
	}{
		{name: "the first measurement", note: "the first measurement, so nothing to compare"},
		{
			name: "overlapping ranges read level",
			previous: previousPerformance(test, performancePreviousCommit, performanceBox,
				performanceSpreadOf(9850, 9600, 10000), performanceSpreadOf(39000, 38000, 40000)),
			note: "against 11111111: cold root wall level (9800 ms, was 9850), user level (39200 ms, was 39000); " +
				"cold deeafbf4 wall level (4100 ms, was 4100), user level (16400 ms, was 16400); " +
				"warm root wall level (800 ms, was 800), user level (3200 ms, was 3200)",
		},
		{
			name: "disjoint ranges read moved",
			previous: previousPerformance(test, performancePreviousCommit, performanceBox,
				performanceSpreadOf(12000, 11900, 12100), performanceSpreadOf(48000, 47600, 48400)),
			note: "against 11111111: cold root wall faster by 2200 ms, 18%, ranges apart (9800 ms, was 12000), " +
				"user faster by 8800 ms, 18%, ranges apart (39200 ms, was 48000); cold deeafbf4 wall level",
		},
		{
			name: "the same binary reads as the box's drift",
			previous: sameBinaryPerformance(test, previousPerformance(test, performancePreviousCommit,
				performanceBox, performanceSpreadOf(12000, 11900, 12100), performanceSpreadOf(48000, 47600, 48400))),
			note: "against 11111111, the same binary (sha256 5f0c3a9e1d2b), so no change to cohere and any " +
				"difference is the box's: cold root wall drifted down by 2200 ms, 18%, the box's ranges apart " +
				"(9800 ms, was 12000), user drifted down by 8800 ms, 18%, the box's ranges apart (39200 ms, was " +
				"48000); cold deeafbf4 wall level",
		},
		{
			name: "another machine reads as its own line",
			previous: previousPerformance(test, performancePreviousCommit, "Marin",
				performanceSpreadOf(9850, 9600, 10000), performanceSpreadOf(39000, 38000, 40000)),
			note: "measured on Shackler (Apple M2 Max, 12 cores), and main's at 11111111 on Marin (Apple M1, " +
				"8 cores): another machine, so not compared",
		},
		{
			name: "a file with no checked counts says so and compares",
			previous: uncountedPerformance(test, previousPerformance(test, performancePreviousCommit,
				performanceBox, performanceSpreadOf(9850, 9600, 10000), performanceSpreadOf(39000, 38000, 40000))),
			note: "against 11111111: main's file records no checked counts; cold root wall level (9800 ms, was " +
				"9850), user level (39200 ms, was 39000); cold deeafbf4 wall level",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newPerformanceFixture(test)
			if testCase.previous != nil {
				writeFixtureFile(test, filepath.Join(fixture.repository, performanceFileName),
					string(testCase.previous))
				fixture.git(fixture.repository, "add", performanceFileName)
				fixture.git(fixture.repository, "commit", "-q", "-m", "main's measurement before")
			}
			writeFixtureFile(test, filepath.Join(fixture.replies, performanceBox+".run"),
				shacklerSays([]int64{9900, 9700, 9800}))
			landed, output, calls := fixture.landing()
			if !landed {
				test.Fatalf("the landing did not land:\n%s", output)
			}
			gatedTree := fixture.treeGated("a")
			if changed := fixture.git(fixture.repository, "diff-tree", "-r", "--name-only", gatedTree,
				"main^{tree}"); changed != performanceFileName {
				test.Errorf("the landing changed %q from the gated tree, want %s alone", changed, performanceFileName)
			}
			contents := fixture.git(fixture.repository, "show", "main:"+performanceFileName)
			if !strings.HasPrefix(contents+"\n", performanceHeader) {
				test.Errorf("the file does not open with its header:\n%s", contents)
			}
			measured := performanceFile{}
			if decodeError := json.Unmarshal([]byte(performanceBody(contents)), &measured); decodeError != nil {
				test.Fatalf("the file does not parse: %v\n%s", decodeError, contents)
			}
			if gated := fixture.git(fixture.repository, "rev-parse", measured.Commit+"^{tree}"); gated != gatedTree {
				test.Errorf("the file names %s, whose tree is %s, not the gated %s", measured.Commit, gated, gatedTree)
			}
			cold, warm := measured.Checks.Cold, measured.Checks.Warm
			for _, check := range []struct {
				what      string
				got, want any
			}{
				{"cold root wall", cold.Root.Wall, performanceSpreadOf(9800, 9700, 9900)},
				{"cold root user", cold.Root.User, performanceSpreadOf(39200, 38800, 39600)},
				{"cold root sys", cold.Root.System, performanceSpreadOf(980, 970, 990)},
				{"cold deeafbf4 wall", cold.Deeafbf4.Wall, performanceSpreadOf(4100, 4000, 4200)},
				{"warm root wall", warm.Root.Wall, performanceSpreadOf(800, 800, 800)},
				{"cold root checked", cold.Root.Checked, &performanceChecked{Files: 120, Findings: 0}},
				{"cold deeafbf4 checked", cold.Deeafbf4.Checked, &performanceChecked{Files: 95, Findings: 7}},
				{"warm root checked", warm.Root.Checked, &performanceChecked{Files: 120, Findings: 0}},
				{"rounds", measured.Checks.Rounds, performanceRounds{Cold: performanceColdRounds,
					Deeafbf4: performanceReferenceRounds, Warm: performanceWarmRounds}},
				{"timeline", measured.Checks.Timeline.Root,
					performanceTimeline{Listing: 812, Handover: 5100, Builds: 6201, Run: 9700}},
				{"machine", fmt.Sprint(measured.Machine), fmt.Sprint(performanceMachine{Name: "Shackler",
					Cpu: "Apple M2 Max", Cores: 12, LoadBefore: []float64{0.89, 0.91, 1.28},
					LoadAfter: []float64{1.1, 0.95, 1.27}, HeldQuiet: true})},
				{"tools", measured.Tools, performanceTools{Cohere: shortCommit(measured.Commit) +
					" CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-s -w', darwin/arm64, on a cache " +
					"the box kept", Go: "go1.27.1", Binary: performanceFixtureBinary}},
				{"schema", measured.Schema, 3},
			} {
				if fmt.Sprint(check.got) != fmt.Sprint(check.want) {
					test.Errorf("the file's %s is %+v, want %+v", check.what, check.got, check.want)
				}
			}
			if !strings.HasPrefix(measured.Note, testCase.note) {
				test.Errorf("the note reads\n%s\nwant it to open\n%s", measured.Note, testCase.note)
			}
			body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main")
			for _, said := range []string{output, body} {
				if !strings.Contains(said, "Measured on Shackler for Performance.jsonc: "+measured.Note+".") {
					test.Errorf("the landing does not say the note:\n%s", said)
				}
			}
			if !strings.Contains(output, "Shackler measured "+shortCommit(measured.Commit)+" in ") ||
				!strings.Contains(output, ": mirror 0.3s, lay 4.1s, toolchain 3.0s, modules 0.6s, build 11.9s, "+
					"cold-root 54.8s, cold-deeafbf4 17.1s, warm-root 9.8s, timeline-root 9.9s") {
				test.Errorf("the landing does not say what each phase cost:\n%s", output)
			}
			remote, readError := os.ReadFile(filepath.Join(fixture.replies, performanceBox+".remote"))
			if readError != nil {
				test.Fatal(readError)
			}
			for _, want := range []string{
				"loom push --feed " + measured.Commit + " --repository cohere",
				"loom gap --box Shackler",
				"loom gap --box Shackler --within 30m -- ssh Shackler " + string(remote),
			} {
				if !slices.Contains(strings.Split(calls, "\n"), want) {
					test.Errorf("ahra was not asked %q:\n%s", want, calls)
				}
			}
			found := regexp.MustCompile(`^echo ([A-Za-z0-9+/=]+) \| base64 -d \| bash -s -- (.*)$`).
				FindSubmatch(remote)
			if found == nil {
				test.Fatalf("the gap's remote is not the encoded script and its arguments:\n%s", remote)
			}
			script, decodeError := base64.StdEncoding.DecodeString(string(found[1]))
			if decodeError != nil {
				test.Fatal(decodeError)
			}
			if string(script) != performanceScript || string(found[2]) != fmt.Sprintf("%s %s %d %d %d %d",
				measured.Commit, performanceReference, performanceFills, performanceColdRounds,
				performanceReferenceRounds, performanceWarmRounds) {
				test.Errorf("the gap ran another script, or handed it %q", found[2])
			}
		})
	}
}

// Shackler held: the land waits in line holding the lock, saying on every wait whose claim it waits behind and since
// when, in her claim's own words, reads her claim again, never runs a gap of its own beside the holder's, and once
// COHERE_DEV_PERFORMANCE_WAIT has passed refuses, naming the holder. Main doesn't move, and the worktree is back.
func TestALandingWaitsForShacklerAndRefusesOnceTheWaitPasses(test *testing.T) {
	test.Parallel()
	fixture := newPerformanceFixture(test)
	claim := "Shackler's hand gap is claimed by system_cohere_go_codebase pair 7, since 01:09Z (14 minutes ago), " +
		"until 01:24Z"
	writeFixtureFile(test, filepath.Join(fixture.replies, performanceBox+".held"), "no gap:\n  "+claim+"\n")
	before := fixture.git(fixture.worktree("a"), "rev-parse", "HEAD")
	// A wait shorter than one read, so the case reads the same at any load: her claim is still read twice.
	landed, output, calls := fixture.landing(performanceWaitVariable + "=1ms")
	if landed {
		test.Fatalf("a landing Shackler never measured landed:\n%s", output)
	}
	for _, want := range []string{
		"cohere-dev: Shackler is held: " + claim + ", exit status 1; the land waits in line holding the lock to " +
			"measure this landing, reading her claim again in ",
		"no measurement from Shackler, and nothing lands unmeasured: Shackler stayed held through ",
		" of waiting: " + claim,
		"the landing was not measured, so nothing landed",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the landing does not say %q:\n%s", want, output)
		}
	}
	reads := 0
	for _, line := range strings.Split(calls, "\n") {
		if line == "loom gap --box Shackler" {
			reads++
		}
		if strings.HasPrefix(line, "loom gap --box Shackler --within") {
			test.Errorf("a gap ran on Shackler while another held her: %s", line)
		}
	}
	if reads < 2 {
		test.Errorf("her claim was read %d times, want at least 2, a wait between:\n%s", reads, calls)
	}
	if after := fixture.git(fixture.worktree("a"), "rev-parse", "HEAD"); after != before {
		test.Errorf("the worktree is at %s, not back at %s", after, before)
	}
}

// A measurement that fails refuses the landing in the box's words, and main doesn't move: the script's own refusal, a
// run at the root that found something the gate never did, a run at deeafbf4 that crashed, a gap that ended badly
// with no refusal said, a row whose rounds checked different files or findings, a run whose summary counts other
// findings than it printed, and a run line that doesn't say what it checked.
func TestAMeasurementThatFailsRefusesTheLanding(test *testing.T) {
	test.Parallel()
	passing := shacklerSays([]int64{9900, 9700, 9800})
	for _, testCase := range []struct {
		name string
		run  string
		exit string
		said string
	}{
		{
			name: "the box refuses",
			run:  performanceRefusedMarker + "cohere didn't build at COMMIT: undefined: cohere\n",
			exit: "1",
			said: "Shackler refused to measure it: cohere didn't build at ",
		},
		{
			name: "a finding at the root",
			run: strings.Replace(passing, "run cold root 2 9700 38800 970 0 0 120 0\n",
				"run cold root 2 9700 38800 970 1 3 120 3\n", 1),
			said: "Shackler's cold run at root, round 2, exited 1 with 3 findings",
		},
		{
			name: "deeafbf4 crashed",
			run: strings.Replace(passing, "run cold deeafbf4 1 4200 16800 420 1 7 95 7\n",
				"run cold deeafbf4 1 4200 16800 420 3 0 95 0\n", 1),
			said: "Shackler's cold run at deeafbf4, round 1, exited 3 with 0 findings",
		},
		{
			name: "rounds that walked different files",
			run: strings.Replace(passing, "run cold root 2 9700 38800 970 0 0 120 0\n",
				"run cold root 2 9700 38800 970 0 0 118 0\n", 1),
			said: "Shackler's cold row at root checked 120 files and 0 findings in round 1, and 118 files and 0 " +
				"findings in round 2, so its rounds measured different things",
		},
		{
			name: "rounds that found different things",
			run: strings.Replace(passing, "run cold deeafbf4 2 4000 16000 400 1 7 95 7\n",
				"run cold deeafbf4 2 4000 16000 400 1 6 95 6\n", 1),
			said: "Shackler's cold row at deeafbf4 checked 95 files and 7 findings in round 1, and 95 files and 6 " +
				"findings in round 2, so its rounds measured different things",
		},
		{
			name: "a summary that counts other findings",
			run: strings.Replace(passing, "run warm root 1 800 3200 80 0 0 120 0\n",
				"run warm root 1 800 3200 80 0 0 120 2\n", 1),
			said: "Shackler's warm run at root, round 1, printed 0 findings and its summary counts 2",
		},
		{
			name: "a run line that doesn't say what it checked",
			run: strings.Replace(passing, "run cold root 2 9700 38800 970 0 0 120 0\n",
				"run cold root 2 9700 38800 970 0 0\n", 1),
			said: `Shackler's run line "cold root 2 9700 38800 970 0 0" doesn't read as a row`,
		},
		{
			name: "a binary with no digest",
			run:  strings.Replace(passing, "binary "+performanceFixtureBinary+"\n", "binary \n", 1),
			said: `Shackler's measurement said the binary's digest as "", not a sha256`,
		},
		{
			name: "a gap that ended badly",
			run:  "ssh: connect to host shackler: Connection refused\n",
			exit: "255",
			said: "Shackler's measurement ended exit status 255 (ssh: connect to host shackler: Connection refused",
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newPerformanceFixture(test)
			writeFixtureFile(test, filepath.Join(fixture.replies, performanceBox+".run"), testCase.run)
			if testCase.exit != "" {
				writeFixtureFile(test, filepath.Join(fixture.replies, performanceBox+".exit"), testCase.exit)
			}
			landed, output, _ := fixture.landing()
			if landed {
				test.Fatalf("a landing whose measurement failed landed:\n%s", output)
			}
			if !strings.Contains(output, "measurement refused the landing, and nothing lands unmeasured: "+
				testCase.said) {
				test.Errorf("the landing does not refuse saying %q:\n%s", testCase.said, output)
			}
		})
	}
}

// A tree with no command/cohere, every other land test's fixture, has no cohere to measure: it lands its gated tree
// as it was, saying so on the land line and in its body, and never asks ahra anything.
func TestALandingWithNoCohereLandsSayingThereIsNothingToMeasure(test *testing.T) {
	test.Parallel()
	fixture := newLandFixture(test)
	replies := test.TempDir()
	ahra := filepath.Join(test.TempDir(), "ahra")
	if writeError := executable_testing.WriteFile(ahra, []byte(macBoxAhra), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	landing := exec.Command(fixture.wrapper, "land")
	landing.Dir = fixture.worktree("a")
	landing.Env = append(fixture.environment, loomStandInVariable+"="+ahra, "GATE_REPLIES="+replies)
	output, landError := landing.CombinedOutput()
	if landError != nil {
		test.Fatalf("the landing failed (%v):\n%s", landError, output)
	}
	gatedTree := fixture.treeGated("a")
	if tree := fixture.git(fixture.repository, "rev-parse", "main^{tree}"); tree != gatedTree {
		test.Errorf("main's tree is %s, not the gated %s", tree, gatedTree)
	}
	commit := regexp.MustCompile(`Nothing measured: ([0-9a-f]{8}) has no command/cohere, so there is no cohere ` +
		`to time\.`).FindStringSubmatch(string(output))
	if commit == nil {
		test.Fatalf("the land line does not say there is nothing to measure:\n%s", output)
	}
	if body := fixture.git(fixture.repository, "log", "-1", "--format=%b", "main"); !strings.Contains(body, commit[0]) {
		test.Errorf("the landing's body does not say there is nothing to measure:\n%s", body)
	}
	if calls := readMaybeMissing(test, filepath.Join(replies, "calls")); len(calls) > 0 {
		test.Errorf("ahra was asked, for a tree with nothing to measure:\n%s", calls)
	}
}

// The note's other lines: another build and another shape of measurement are each a line of their own, a row that
// checked other files or findings than main's same row is a line of its own while the rows that checked the same
// compare, and a file main holds that doesn't read is named, never compared. A level range touching the old one at its
// edge is level, and moved says by how much of the old median.
func TestThePerformanceNoteComparesOnlyLikeWithLike(test *testing.T) {
	test.Parallel()
	measurement, problem := readPerformance([]byte(strings.ReplaceAll(shacklerSays([]int64{9700, 9800, 9900}),
		"COMMIT", performancePreviousCommit)), performancePreviousCommit, nil)
	if problem != "" {
		test.Fatal(problem)
	}
	current := performanceFileOf(measurement, performancePreviousCommit, time.Unix(0, 0))
	otherBuild, otherShape, otherChecked, sameBinary, otherBinary := current, current, current, current, current
	otherBuild.Tools.Go = "go1.27.2"
	otherShape.Checks.Rounds.Cold = 5
	// A file written before keys were spelled whole, schema 1, reads as another shape, its min and max never read.
	oldSpelling := current
	oldSpelling.Schema = 1
	otherChecked.Checks.Cold.Deeafbf4.Checked = &performanceChecked{Files: 2210, Findings: 1100}
	otherChecked.Tools.Binary = performanceMainBinary
	// main's file measured the same binary a little faster at the root, and another binary just the same.
	sameBinary.Checks.Cold.Root.Wall = performanceSpreadOf(9500, 9450, 9550)
	otherBinary.Checks.Cold.Root.Wall = sameBinary.Checks.Cold.Root.Wall
	otherBinary.Tools.Binary = performanceMainBinary
	for _, row := range []struct {
		name     string
		previous performanceFile
		unread   string
		want     string
	}{
		{"another build", otherBuild, "", "built as CGO_ENABLED=0 go build -trimpath -buildvcs=false " +
			"-ldflags='-s -w', darwin/arm64, on a cache the box kept with go1.27.1, and main's at 11111111 as " +
			"CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-s -w', darwin/arm64, on a cache the box " +
			"kept with go1.27.2: another build, so not compared"},
		{"another shape", otherShape, "", fmt.Sprintf("main's at 11111111 was measured in another shape (schema 3, "+
			"rounds 5/2/%d), so not compared", performanceWarmRounds)},
		{"the old spelling", oldSpelling, "", fmt.Sprintf("main's at 11111111 was measured in another shape "+
			"(schema 1, rounds %d/%d/%d), so not compared", performanceColdRounds, performanceReferenceRounds,
			performanceWarmRounds)},
		{"a file that doesn't read", current, "unexpected end of JSON input", "main's Performance.jsonc didn't " +
			"read (unexpected end of JSON input), so nothing is compared"},
		{"a row that checked otherwise", otherChecked, "", "against 11111111: cold root wall level (9800 ms, was " +
			"9800), user level (39200 ms, was 39200); cold deeafbf4 checked 95 files and 7 findings, and main's 2210 " +
			"and 1100, so not compared; warm root wall level (800 ms, was 800), user level (3200 ms, was 3200)"},
		{"the same binary", sameBinary, "", "against 11111111, the same binary (sha256 5f0c3a9e1d2b), so no change " +
			"to cohere and any difference is the box's: cold root wall drifted up by 300 ms, 3%, the box's " +
			"ranges apart (9800 ms, was 9500), user level (39200 ms, was 39200); cold deeafbf4 wall level (4100 ms, " +
			"was 4100), user level (16400 ms, was 16400); warm root wall level (800 ms, was 800), user level (3200 " +
			"ms, was 3200)"},
		{"another binary", otherBinary, "", "against 11111111: cold root wall slower by 300 ms, 3%, ranges apart " +
			"(9800 ms, was 9500), user level (39200 ms, was 39200); cold deeafbf4 wall level (4100 ms, was 4100), " +
			"user level (16400 ms, was 16400); warm root wall level (800 ms, was 800), user level (3200 ms, was 3200)"},
	} {
		if note := performanceNote(current, row.previous, true, row.unread); note != row.want {
			test.Errorf("%s: the note reads\n%s\nwant\n%s", row.name, note, row.want)
		}
	}
	for _, row := range []struct {
		now, before performanceSpread
		want        string
	}{
		{performanceSpreadOf(100, 90, 110), performanceSpreadOf(120, 110, 130), "level (100 ms, was 120)"},
		{performanceSpreadOf(100, 90, 109), performanceSpreadOf(120, 110, 130),
			"faster by 20 ms, 17%, ranges apart (100 ms, was 120)"},
		{performanceSpreadOf(150, 140, 160), performanceSpreadOf(120, 110, 130),
			"slower by 30 ms, 25%, ranges apart (150 ms, was 120)"},
	} {
		if said := spreadCompared(row.now, row.before); said != row.want {
			test.Errorf("%+v against %+v reads %q, want %q", row.now, row.before, said, row.want)
		}
	}
}

// The measurement's script, run with bash against mirrors and a toolchain of its own (#6wnsnq2): it waits for the
// mirror, lays the root at the commit and deeafbf4 at its own, each with TypeScript at its pin, under
// ~/scratch/system_cohere_tools/performance, checks the toolchain by its digest, builds the binary on the Go cache it
// keeps there, and runs cohere 2 fills and 3 rounds cold at the root, 2 and 2 at deeafbf4, 1 and 1 warm and one
// timeline run, each over the binary's own system store under /usr/bin/time -l, saying each in lines readPerformance
// reads as a whole measurement, with the files walked and findings cohere's own records say. Run again, it keeps
// deeafbf4 as laid, and clears what the runs before left in either tree. Both runs leave no configuration file behind.
// A run whose cohere printed no lint record or no summary refuses, naming its row and cohere's last words.
func TestThePerformanceScriptMeasuresInItsScratch(test *testing.T) {
	test.Parallel()
	needs.Platform(test, "darwin", "the script reads a Mac's sysctl and /usr/bin/time -l, and Shackler is one")
	needs.Tool(test, "bash", "the script is a bash script")
	directory := test.TempDir()
	environment := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com")
	git := func(where string, arguments ...string) string {
		test.Helper()
		command := exec.Command("git", arguments...)
		command.Dir, command.Env = where, environment
		output, gitError := command.CombinedOutput()
		if gitError != nil {
			test.Fatalf("git %v in %s: %v\n%s", arguments, where, gitError, output)
		}
		return strings.TrimSpace(string(output))
	}
	// TypeScript, its one commit the pin, and cohere, whose commits record it as a gitlink.
	typeScript := filepath.Join(directory, "TypeScript")
	writeFixtureFile(test, filepath.Join(typeScript, "README.md"), "TypeScript\n")
	git(typeScript, "init", "-q", "-b", "main")
	git(typeScript, "add", ".")
	git(typeScript, "commit", "-q", "-m", "TypeScript")
	pin := git(typeScript, "rev-parse", "HEAD")
	source := filepath.Join(directory, "cohere")
	writeFixtureFile(test, filepath.Join(source, "command", "cohere", "main.go"), "package main\n")
	git(source, "init", "-q", "-b", "main")
	git(source, "add", ".")
	git(source, "update-index", "--add", "--cacheinfo", "160000,"+pin+",TypeScript")
	git(source, "commit", "-q", "-m", "the older cohere")
	reference := git(source, "rev-parse", "HEAD")
	writeFixtureFile(test, filepath.Join(source, "command", "cohere", "landed.go"), "package main\n")
	git(source, "add", "command")
	git(source, "commit", "-q", "-m", "the cohere being landed")
	commit := git(source, "rev-parse", "HEAD")
	mirrors := filepath.Join(directory, "mirrors")
	for name, from := range map[string]string{"cohere.git": source, "TypeScript.git": typeScript} {
		git(directory, "init", "-q", "--bare", filepath.Join(mirrors, name))
		git(from, "push", "-q", filepath.Join(mirrors, name), "HEAD:refs/heads/main")
	}

	// A toolchain whose go answers what the script asks, records each build, and builds a stand-in for cohere that
	// records each run, walks 2 files at the root and finds 1 thing in 1 file at deeafbf4, exiting 1 there as the
	// real one does, its lint and summary records saying so. With PERFORMANCE_COHERE_UNLINTED set it prints no lint
	// record, and with PERFORMANCE_COHERE_UNSUMMARIZED no summary.
	builds := filepath.Join(directory, "builds")
	runs := filepath.Join(directory, "runs")
	binary := filepath.Join(directory, "stand-in-cohere")
	binaryScript := "#!/bin/sh\necho \"$PWD $COHERE_SYSTEM_CACHE $COHERE_GO_CHILD $*\" >> '" + runs + "'\n" +
		"root=''; timeline=''\n" +
		"while [ \"$#\" -gt 0 ]; do case \"$1\" in --root) root=\"$2\"; shift ;; --timeline) timeline=1 ;; esac; " +
		"shift; done\n" +
		"[ -z \"$timeline\" ] || echo '{\"kind\":\"timeline\",\"stamps\":[{\"label\":\"host.listing.end\"," +
		"\"wallMicroseconds\":1500,\"userMicroseconds\":0,\"systemMicroseconds\":0},{\"label\":\"run.end\"," +
		"\"wallMicroseconds\":4000,\"userMicroseconds\":0,\"systemMicroseconds\":0}]}'\n" +
		"[ -z \"$PERFORMANCE_COHERE_UNLINTED\" ] || { echo 'the lint phase never ran' >&2; " +
		"echo '{\"kind\":\"summary\",\"findings\":0,\"complete\":false,\"exitCode\":0}'; exit 0; }\n" +
		"[ -z \"$PERFORMANCE_COHERE_UNSUMMARIZED\" ] || { echo 'cohere was killed' >&2; " +
		"echo '{\"kind\":\"lint\",\"findings\":0,\"rulesRun\":237,\"filesWalked\":2,\"nodesVisited\":80}'; " +
		"exit 0; }\n" +
		"case \"$root\" in */deeafbf4) echo '{\"kind\":\"finding\"}'; " +
		"echo '{\"kind\":\"lint\",\"findings\":1,\"rulesRun\":237,\"filesWalked\":1,\"nodesVisited\":40}'; " +
		"echo '{\"kind\":\"summary\",\"findings\":1,\"complete\":true,\"exitCode\":1}'; exit 1 ;; esac\n" +
		"echo '{\"kind\":\"lint\",\"findings\":0,\"rulesRun\":237,\"filesWalked\":2,\"nodesVisited\":80}'\n" +
		"echo '{\"kind\":\"summary\",\"findings\":0,\"complete\":true,\"exitCode\":0}'\n"
	writeFixtureFile(test, binary, binaryScript)
	goroot := filepath.Join(directory, "go")
	goScript := "#!/bin/sh\ncase \"$1\" in\nmod) exit 0 ;;\n" +
		"env) case \"$2\" in GOOS) echo darwin ;; GOARCH) echo arm64 ;; GOVERSION) echo go1.27.1 ;; esac; exit 0 ;;\n" +
		"build) echo \"GOCACHE=$GOCACHE CGO_ENABLED=$CGO_ENABLED $*\" >> '" + builds + "'\n" +
		"  while [ \"$#\" -gt 0 ]; do [ \"$1\" != -o ] || output=\"$2\"; shift; done\n" +
		"  cp '" + binary + "' \"$output\"; chmod +x \"$output\"; exit 0 ;;\nesac\n" +
		"echo \"the stand-in go has no answer for: $*\" >&2; exit 2\n"
	if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(goScript),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	script := strings.Replace(performanceScript, macBoxMirrorLine, "mirrors="+mirrors+"; tries=2; pause=0", 1)
	script = strings.Replace(script, `goroot="`+macBoxGoroot+`"; digest="`+macBoxToolchainDigest+`"`,
		`goroot="`+goroot+`"; digest="`+toolchainDigestOf(test, goroot)+`"`, 1)
	if strings.Count(script, mirrors) != 1 || strings.Count(script, goroot) != 1 {
		test.Fatal("the script's mirror line or toolchain was not found to replace")
	}
	home := filepath.Join(directory, "home")
	scratch := filepath.Join(home, "scratch", "system_cohere_tools", "performance")
	// read runs the script once, with extra in its environment, and reads what it said.
	read := func(extra ...string) ([]byte, performanceMeasurement, string) {
		test.Helper()
		bash := exec.Command("bash", "-s", "--", commit, reference, "2", "3", "2", fmt.Sprint(performanceWarmRounds))
		bash.Dir, bash.Stdin = test.TempDir(), strings.NewReader(script)
		bash.Env = slices.Concat(environment, []string{"HOME=" + home}, extra)
		output, bashError := bash.CombinedOutput()
		measurement, problem := readPerformance(output, commit, bashError)
		return output, measurement, problem
	}
	measure := func() performanceMeasurement {
		test.Helper()
		output, measurement, problem := read()
		if problem != "" {
			test.Fatalf("the script's measurement didn't read: %s\n%s", problem, output)
		}
		if _, statError := os.Stat(filepath.Join(scratch, macBoxSafeSettings)); !os.IsNotExist(statError) {
			test.Errorf("the script left its configuration file behind: %v", statError)
		}
		return measurement
	}
	measurement := measure()
	for tree, want := range map[string]string{"root": commit, "deeafbf4": reference} {
		if at := git(filepath.Join(scratch, tree), "rev-parse", "HEAD"); at != want {
			test.Errorf("%s is laid at %s, want %s", tree, at, want)
		}
		if at := git(filepath.Join(scratch, tree, "TypeScript"), "rev-parse", "HEAD"); at != pin {
			test.Errorf("%s's TypeScript is at %s, want the pin %s", tree, at, pin)
		}
	}
	if built := strings.TrimSpace(string(readMaybeMissing(test, builds))); built != "GOCACHE="+
		filepath.Join(scratch, "gocache")+" CGO_ENABLED=0 build -trimpath -buildvcs=false -ldflags=-s -w -o "+
		filepath.Join(scratch, "cohere")+" ./command/cohere" {
		test.Errorf("the binary was built as %q, want the release's Go build on the cache the scratch keeps", built)
	}
	ran := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, runs))), "\n")
	// Each cold row's fills and rounds, the warm row's one fill and rounds, and the timeline run.
	want := performanceFills + performanceColdRounds + performanceFills + performanceReferenceRounds + 1 +
		performanceWarmRounds + 1
	if len(ran) != want {
		test.Errorf("cohere ran %d times, want %d, fills included:\n%s", len(ran), want, strings.Join(ran, "\n"))
	}
	for _, line := range ran {
		if !strings.Contains(line, " "+filepath.Join(scratch, "system")+" 1 go-language --contract 4 --root ") {
			test.Errorf("a run was not cohere over the binary's own system store: %s", line)
		}
	}
	for _, run := range measurement.runs {
		filesWalked, findings := 2, 0
		if run.tree == performanceReferenceTree {
			filesWalked, findings = 1, 1
		}
		if run.filesWalked != filesWalked || run.findingsPrinted != findings || run.findingsSummarized != findings {
			test.Errorf("the %s run at %s, round %s, said %d files walked and %d findings, %d by its summary, want "+
				"%d and %d", run.mode, run.tree, run.round, run.filesWalked, run.findingsPrinted,
				run.findingsSummarized, filesWalked, findings)
		}
	}
	if fmt.Sprintf("%s %d", measurement.timeline.Stamps[0].Label, len(measurement.phases)) != "host.listing.end 9" ||
		measurement.cores <= 0 || measurement.cpu == "" {
		test.Errorf("the measurement read %d phases, cpu %q with %d cores and stamps %+v", len(measurement.phases),
			measurement.cpu, measurement.cores, measurement.timeline.Stamps)
	}

	for _, tree := range []string{"root", "deeafbf4"} {
		writeFixtureFile(test, filepath.Join(scratch, tree, ".cache", "left"), "a store a run before left\n")
	}
	writeFixtureFile(test, filepath.Join(scratch, "deeafbf4", "kept"), "laid once\n")
	measure()
	for _, tree := range []string{"root", "deeafbf4"} {
		if _, statError := os.Stat(filepath.Join(scratch, tree, ".cache")); !os.IsNotExist(statError) {
			test.Errorf("%s keeps the store the runs before left: %v", tree, statError)
		}
	}
	if _, statError := os.Stat(filepath.Join(scratch, "deeafbf4", "kept")); statError != nil {
		test.Errorf("deeafbf4 was laid again rather than kept: %v", statError)
	}

	for _, unrecorded := range []struct{ variable, want string }{
		{"PERFORMANCE_COHERE_UNLINTED", "no lint record: the lint phase never ran"},
		{"PERFORMANCE_COHERE_UNSUMMARIZED", "no summary record: cohere was killed"},
	} {
		output, _, problem := read(unrecorded.variable + "=1")
		if want := "Shackler refused to measure it: the cold run at root, round fill, printed " +
			unrecorded.want; problem != want {
			test.Errorf("a run with %s read as %q, want %q:\n%s", unrecorded.variable, problem, want, output)
		}
	}
}

// toolchainDigestOf is sharedBoxToolchainCheck's digest of the tree at goroot, read here in Go: each regular file's
// sha256 and its path from the root, as sha256sum lists them in byte order, through sha256 again.
func toolchainDigestOf(test *testing.T, goroot string) string {
	test.Helper()
	paths := []string{}
	walkError := filepath.WalkDir(goroot, func(path string, entry fs.DirEntry, pathError error) error {
		if pathError != nil || !entry.Type().IsRegular() {
			return pathError
		}
		relative, relativeError := filepath.Rel(goroot, path)
		paths = append(paths, "./"+filepath.ToSlash(relative))
		return relativeError
	})
	if walkError != nil {
		test.Fatal(walkError)
	}
	sort.Strings(paths)
	var listing strings.Builder
	for _, path := range paths {
		contents, readError := os.ReadFile(filepath.Join(goroot, filepath.FromSlash(path)))
		if readError != nil {
			test.Fatal(readError)
		}
		sum := sha256.Sum256(contents)
		listing.WriteString(hex.EncodeToString(sum[:]) + "  " + path + "\n")
	}
	sum := sha256.Sum256([]byte(listing.String()))
	return hex.EncodeToString(sum[:])
}
