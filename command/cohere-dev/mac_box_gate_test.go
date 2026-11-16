//go:build unix

package main

import (
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
	"github.com/system-inc/cohere/internal/needs"
)

// macBoxFixture is a landing ready to gate on the house Macs: a module with one package and one test, and a
// raceGated package when asked, ahra stood in for by a script that answers the push and each box's gap from files in
// replies, and go stood in for by one that records each call, so a test can read what Kirk's Mac ran.
type macBoxFixture struct {
	*landFixture
	// replies holds what the stand-in ahra answers: `push.fails` makes the push fail, `<box>.held` makes the box's
	// claim read held, `<box>.run` is what her gap prints, with `COMMIT` the commit the remote names, and `<box>.exit`
	// its exit. The stand-in writes each call it took to `calls`, one line each, and each box's remote command whole
	// to `<box>.remote`.
	replies string
	// goCalls is every go command Kirk's Mac ran, one line each.
	goCalls string
}

// macBoxAhra stands in for ahra, answering from the replies directory it names as macBoxFixture says. A gap's
// arguments are `loom gap --box <box> --within <bound> -- ssh <box> <remote>`, so its remote is the tenth, and a
// claim's read is `loom gap --box <box>` alone.
const macBoxAhra = `#!/bin/sh
replies="$GATE_REPLIES"
printf '%s\n' "$*" >> "$replies/calls"
case "$2" in
push)
  if [ -e "$replies/push.fails" ]; then echo "hop 1 into Zach's feed failed: the feed refused it"; exit 1; fi
  echo "Zach's feed cohere.git holds it"; exit 0 ;;
gap)
  box="$4"
  if [ "$#" -eq 4 ]; then
    if [ -e "$replies/$box.held" ]; then echo "gap $box closed · $box is held by system_loom until 18:00Z"; exit 1; fi
    echo "gap $box open"; exit 0
  fi
  printf '%s' "${10}" > "$replies/$box.remote"
  commit=$(printf '%s' "${10}" | sed 's/.* bash -s -- \([0-9a-f]*\) .*/\1/')
  sed "s/COMMIT/$commit/g" "$replies/$box.run"
  exit "$(cat "$replies/$box.exit" 2>/dev/null || echo 0)" ;;
esac
echo "the stand-in has no answer for: $*"; exit 2
`

func newMacBoxFixture(test *testing.T, raced bool) *macBoxFixture {
	test.Helper()
	fixture := &macBoxFixture{landFixture: newLandFixture(test), replies: test.TempDir()}
	stand := test.TempDir()
	fixture.goCalls = filepath.Join(stand, "go")
	recorder := "#!/bin/sh\necho \"$*\" >> '" + fixture.goCalls + "'\n"
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"),
		[]byte(recorder), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	ahra := filepath.Join(stand, "ahra")
	if writeError := executable_testing.WriteFile(ahra, []byte(macBoxAhra), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	// A short wait in line, so a landing that wrongly finds every box held refuses in seconds rather than holding the
	// test for gateWaitDefault. A case that means the wait sets its own, later, which wins.
	fixture.environment = append(fixture.environment, gateVariable+"=on", loomStandInVariable+"="+ahra,
		"GATE_REPLIES="+fixture.replies, gateWaitVariable+"=5s")
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
	writeFixtureFile(test, filepath.Join(worktree, "alpha", "alpha_test.go"),
		"package alpha\n\nimport \"testing\"\n\nfunc TestAlpha(test *testing.T) {}\n")
	if raced {
		// A package at one of raceGated's directories, so the gate asks the box for its -race run.
		writeFixtureFile(test, filepath.Join(worktree, "internal", "edit", "edit_test.go"),
			"package edit\n\nimport \"testing\"\n\nfunc TestEdit(test *testing.T) {}\n")
	}
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a module"))
	fixture.git(worktree, "merge", "-q", "--no-edit", "-m", "Merge main", "main")
	return fixture
}

// reply writes what the stand-in ahra answers from file in the replies directory.
func (fixture *macBoxFixture) reply(file string, contents string) {
	fixture.test.Helper()
	writeFixtureFile(fixture.test, filepath.Join(fixture.replies, file), contents)
}

// landing lands the fixture's worktree with environment added, and returns whether main moved, everything the landing
// said, every call the stand-in ahra took and every go command Kirk's Mac ran.
func (fixture *macBoxFixture) landing(environment ...string) (bool, string, string, string) {
	fixture.test.Helper()
	before := fixture.git(fixture.repository, "rev-parse", "main")
	landing := exec.Command(fixture.wrapper, "land", "--message", "a module tested on a gate box")
	landing.Dir, landing.Env = fixture.worktree("a"), append(fixture.environment, environment...)
	// Ignored: whether it landed is read from main, and what it said is the output.
	output, _ := landing.CombinedOutput()
	landed := fixture.git(fixture.repository, "rev-parse", "main") != before
	return landed, string(output), string(readMaybeMissing(fixture.test, filepath.Join(fixture.replies, "calls"))),
		string(readMaybeMissing(fixture.test, fixture.goCalls))
}

// macBoxPassing is a box's run that passed, on darwin/arm64, its one package's.
const macBoxPassing = boxMarker + "COMMIT on darwin/arm64\n" +
	`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n"

// A landing's tests run on the first gate box that is free, and only there (#rr818c8). Sun2 passing lands it, naming
// her, her package count and her skips as run nowhere on the land line. Sun2 held passes to Marin, saying so. Every
// box held waits in line, then refuses, naming each box. A box with no verdict, a box's failing test and a mirror that
// lacks the commit each refuse it in the box's words. Shackler named in the setting is refused by name before anything
// is asked. And none of them runs a go test on Kirk's Mac, which only the escape does, saying so on the land line.
func TestALandingTestsOnTheGatesMacsAndNeverOnKirksMac(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		name string
		// replies are the files the stand-in ahra answers from, each named for its box.
		replies     map[string]string
		environment []string
		raced       bool
		lands       bool
		inBody      []string
		said        []string
		// asked are calls the stand-in ahra took, each a whole line, and unasked prefixes of lines it never took.
		asked   []string
		unasked []string
		// claimReads is how many times at least each box's claim was read.
		claimReads map[string]int
		// kirksMac is whether Kirk's Mac runs the tests, which only the escape may.
		kirksMac bool
	}{
		{
			name: "Sun2 passes",
			replies: map[string]string{"Sun2.run": boxMarker + "COMMIT on darwin/arm64\n" +
				`{"Action":"pass","Package":"example.com/fixture/alpha"}` + "\n" +
				`{"Action":"skip","Package":"example.com/fixture/alpha","Test":"TestPlantedSkip"}` + "\n" +
				`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n" +
				boxRaceMarker + "\n" +
				`{"Action":"pass","Package":"example.com/fixture/internal/edit"}` + "\n"},
			raced: true,
			lands: true,
			inBody: []string{"go test of the module on Sun2 (darwin/arm64, 2 packages, scratch box, shared uid), 1 " +
				"of them again under -race, 1 skipped on Sun2 and run nowhere: TestPlantedSkip " +
				"(example.com/fixture/alpha)"},
			asked:   []string{"loom gap --box Sun2"},
			unasked: []string{"loom gap --box Marin"},
		},
		{
			name:    "Sun2 held, then Marin passes",
			replies: map[string]string{"Sun2.held": "", "Marin.run": macBoxPassing},
			lands:   true,
			inBody: []string{"go test of the module on Marin (darwin/arm64, 1 packages, scratch box, shared uid), " +
				"nothing it skipped"},
			said:    []string{"Sun2 is held: Sun2 (gap Sun2 closed · Sun2 is held by system_loom until 18:00Z"},
			asked:   []string{"loom gap --box Sun2", "loom gap --box Marin"},
			unasked: []string{"loom gap --box Sun2 --within"},
		},
		{
			name:        "every box held",
			replies:     map[string]string{"Sun2.held": "", "Marin.held": ""},
			environment: []string{gateWaitVariable + "=2s"},
			said: []string{"every gate box is held (Sun2, Marin); the land waits in line holding the lock",
				"no verdict from a gate box, so nothing landed, and its tests never fall back to Kirk's Mac: every " +
					"gate box stayed held through 2s of waiting: Sun2 (gap Sun2 closed",
				"; Marin (gap Marin closed"},
			unasked:    []string{"loom gap --box Sun2 --within", "loom gap --box Marin --within"},
			claimReads: map[string]int{"Sun2": 2, "Marin": 2},
		},
		{
			name: "Sun2 gives no verdict",
			replies: map[string]string{"Sun2.run": "ssh: connect to host sun2: Connection refused\n",
				"Sun2.exit": "255"},
			said: []string{"never fall back to Kirk's Mac: Sun2's run never said what it tested (ssh: connect to " +
				"host sun2: Connection refused, exit status 255)"},
			unasked: []string{"loom gap --box Marin"},
		},
		{
			name: "Sun2 fails a test",
			replies: map[string]string{"Sun2.run": boxMarker + "COMMIT on darwin/arm64\n" +
				`{"Action":"fail","Package":"example.com/fixture/alpha","Test":"TestPlantedFailure"}` + "\n" +
				`{"Action":"fail","Package":"example.com/fixture/alpha"}` + "\n", "Sun2.exit": "1"},
			said:    []string{"Sun2's tests on darwin/arm64 failed, so nothing landed", "TestPlantedFailure"},
			unasked: []string{"loom gap --box Marin"},
		},
		{
			name: "Sun2's mirror lacks the commit",
			replies: map[string]string{"Sun2.run": sharedBoxRefusedMarker + macBoxMirrorLacks + "COMMIT\n",
				"Sun2.exit": "1"},
			said:    []string{"Sun2 refused to test this landing, so nothing landed: this box's mirror doesn't hold "},
			unasked: []string{"loom gap --box Marin"},
		},
		{
			name:        "Shackler named in the setting",
			replies:     map[string]string{"Sun2.run": macBoxPassing},
			environment: []string{gateBoxesVariable + "=Sun2, shackler"},
			said:        []string{"COHERE_DEV_GATE_BOXES names Shackler, who holds numbers of record"},
			unasked:     []string{"loom"},
		},
		{
			name:        "the escape",
			environment: []string{gateVariable + "=" + gateOffSetting},
			lands:       true,
			inBody:      []string{"go test of the module on Kirk's Mac, since COHERE_DEV_GATE=off"},
			unasked:     []string{"loom"},
			kirksMac:    true,
		},
	} {
		test.Run(testCase.name, func(test *testing.T) {
			test.Parallel()
			fixture := newMacBoxFixture(test, testCase.raced)
			for file, contents := range testCase.replies {
				fixture.reply(file, contents)
			}
			landed, output, calls, goCalls := fixture.landing(testCase.environment...)
			macTests := 0
			for _, line := range strings.Split(goCalls, "\n") {
				if strings.HasPrefix(line, "test") {
					macTests++
				}
			}
			if testCase.kirksMac != (macTests > 0) {
				test.Errorf("Kirk's Mac ran %d go tests, want them run there %t:\n%s", macTests, testCase.kirksMac,
					goCalls)
			}
			if landed != testCase.lands {
				test.Fatalf("landed: %t, want %t:\n%s", landed, testCase.lands, output)
			}
			for _, want := range testCase.said {
				if !strings.Contains(output, want) {
					test.Errorf("the landing does not say %q:\n%s", want, output)
				}
			}
			lines := strings.Split(calls, "\n")
			for _, want := range testCase.asked {
				if !slices.Contains(lines, want) {
					test.Errorf("ahra was not asked %q:\n%s", want, calls)
				}
			}
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
					test.Errorf("%s's claim was read %d times, want at least %d, a wait between:\n%s", box, reads,
						least, calls)
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

// macBoxRemote is a gap's remote command: the encoded script through bash, then its arguments, the commit, the pin and
// the races quoted, then the patterns.
var macBoxRemote = regexp.MustCompile(
	`^echo ([A-Za-z0-9+/=]+) \| base64 -d \| bash -s -- ([0-9a-f]{40}) '([0-9a-f]*)' '([^']*)' (.*)$`,
)

// The push puts the commit being landed in Loom's feed for cohere, the gap runs on Sun2 within its bound over ssh to
// her, and the script it carries, decoded from the recorded call, works under the scratch directory alone, fetches
// from the Macs' mirrors, and runs sharedBoxGate against go1.27.1 darwin/arm64 and node v24.19.0 darwin-arm64 as the
// Macs hold them, by their digests read on Sun2 and Marin.
func TestTheGatesMacScriptCarriesTheMacsPinsAndItsScratch(test *testing.T) {
	test.Parallel()
	fixture := newMacBoxFixture(test, false)
	fixture.reply("Sun2.run", macBoxPassing)
	landed, output, calls, _ := fixture.landing()
	if !landed {
		test.Fatalf("the landing did not land:\n%s", output)
	}
	remote, readError := os.ReadFile(filepath.Join(fixture.replies, "Sun2.remote"))
	if readError != nil {
		test.Fatal(readError)
	}
	found := macBoxRemote.FindSubmatch(remote)
	if found == nil {
		test.Fatalf("the gap's remote is not the encoded script and its arguments:\n%s", remote)
	}
	commit, pin, races, patterns := string(found[2]), string(found[3]), string(found[4]), string(found[5])
	if fixture.git(fixture.repository, "rev-parse", commit+"^{tree}") !=
		fixture.git(fixture.repository, "rev-parse", "main^{tree}") || pin != "" || races != "" || patterns != "./..." {
		test.Errorf("the script was handed commit %s, pin %q, races %q and patterns %q", commit, pin, races, patterns)
	}
	for _, want := range []string{
		"loom push --feed " + commit + " --repository cohere",
		"loom gap --box Sun2 --within 1h -- ssh Sun2 " + string(remote),
	} {
		if !slices.Contains(strings.Split(calls, "\n"), want) {
			test.Errorf("ahra was not asked %q:\n%s", want, calls)
		}
	}
	script, decodeError := base64.StdEncoding.DecodeString(string(found[1]))
	if decodeError != nil {
		test.Fatal(decodeError)
	}
	for _, want := range []string{
		"set -euo pipefail\n",
		"\nbase=~/scratch/system_cohere_tools\nmkdir -p \"$base\"; cd \"$base\"\n",
		"mirrors=/opt/loom-mirrors;",
		`fetch -q "$mirrors/cohere.git" "$commit"`,
		`fetch -q "$mirrors/TypeScript.git" "$pin"`,
		`GIT_CONFIG_GLOBAL="$safe"`,
		"\ngoroot=\"/opt/loom-tools/go\"; " +
			"digest=\"4256a1b77627be7a90f81396a371991e6232f1d1f0d55df55d4ad1f58f43405b\"\n",
		"\nnode=\"/opt/loom-tools/node-v24.19.0\"; " +
			"nodeDigest=\"27db838bb204ef7c21df2931f5656e4c8fb32e6e947f363a402b49714d32b5b1\"\n",
	} {
		if !strings.Contains(string(script), want) {
			test.Errorf("the box's script lacks %q:\n%s", want, script)
		}
	}
	if !strings.HasSuffix(string(script), sharedBoxGate) {
		test.Errorf("the box's script does not end with sharedBoxGate, unchanged:\n%s", script)
	}
}

// The box's script, run with bash against mirrors of its own: a mirror that lacks the commit is asked again and then
// refused in the marker's words, a mirror that can't be read is refused at once in git's, and a mirror holding the
// commit has it fetched and checked out under ~/scratch/system_cohere_tools, reaching sharedBoxGate, which here finds
// no toolchain. Each leaves no configuration file of its own behind.
func TestTheGatesMacScriptWaitsForItsMirrorAndWorksInItsScratch(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the box's script is a bash script")
	source := test.TempDir()
	mirrors := test.TempDir()
	run := func(directory string, arguments ...string) string {
		test.Helper()
		git := exec.Command("git", arguments...)
		git.Dir = directory
		git.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
		output, gitError := git.CombinedOutput()
		if gitError != nil {
			test.Fatalf("git %v: %v\n%s", arguments, gitError, output)
		}
		return strings.TrimSpace(string(output))
	}
	writeFixtureFile(test, filepath.Join(source, "go.mod"), "module example.com/module\n\ngo 1.27\n")
	run(source, "init", "-q")
	run(source, "add", ".")
	run(source, "-c", "user.name=test", "-c", "user.email=test@example.com", "commit", "-q", "-m", "a module")
	commit := run(source, "rev-parse", "HEAD")
	run(mirrors, "init", "-q", "--bare", "cohere.git")
	run(source, "push", "-q", filepath.Join(mirrors, "cohere.git"), "HEAD:refs/heads/main")
	noGo := filepath.Join(test.TempDir(), "no-go")
	for _, row := range []struct {
		name    string
		mirrors string
		commit  string
		said    string
	}{
		{"a mirror lacking the commit", mirrors, strings.Repeat("0", 40),
			sharedBoxRefusedMarker + macBoxMirrorLacks + strings.Repeat("0", 40)},
		{"a mirror that can't be read", filepath.Join(mirrors, "missing"), commit,
			sharedBoxRefusedMarker + filepath.Join(mirrors, "missing") + "/cohere.git didn't read: "},
		{"a mirror holding the commit", mirrors, commit, sharedBoxRefusedMarker + "there is no go at " + noGo},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			home := test.TempDir()
			script := strings.Replace(macBoxScript, macBoxMirrorLine, "mirrors="+row.mirrors+"; tries=2; pause=0", 1)
			script = strings.Replace(script, `goroot="`+macBoxGoroot+`"`, `goroot="`+noGo+`"`, 1)
			if script == macBoxScript {
				test.Fatal("the script's mirror line or goroot was not found to replace")
			}
			bash := exec.Command("bash", "-s", "--", row.commit, "", "", "./...")
			bash.Dir = test.TempDir()
			bash.Stdin = strings.NewReader(script)
			bash.Env = append(os.Environ(), "HOME="+home, "GIT_CONFIG_NOSYSTEM=1")
			output, bashError := bash.CombinedOutput()
			if bashError == nil || !strings.Contains(string(output), row.said) {
				test.Fatalf("the script did not refuse, saying %q (%v):\n%s", row.said, bashError, output)
			}
			scratch := filepath.Join(home, "scratch", "system_cohere_tools")
			if _, statError := os.Stat(filepath.Join(scratch, macBoxSafeSettings)); !os.IsNotExist(statError) {
				test.Errorf("the script left its configuration file behind: %v", statError)
			}
			if row.commit != commit || row.mirrors != mirrors {
				return
			}
			if checkedOut := run(filepath.Join(scratch, "tree"), "rev-parse", "HEAD"); checkedOut != commit {
				test.Errorf("the scratch tree is at %s, want %s", checkedOut, commit)
			}
		})
	}
}

// The setting of the gate's boxes is a list tried in order, Sun2 then Marin unless it says, and it never names
// Shackler, whatever its case; the wait is Go's duration, 2 hours unless it says.
func TestTheGatesBoxesAndItsWaitAreSettings(test *testing.T) {
	test.Parallel()
	for _, row := range []struct {
		setting string
		boxes   string
		refusal string
	}{
		{"", "Sun2,Marin", ""},
		{"Marin, Sun1", "Marin,Sun1", ""},
		{"Sun2,,Marin,", "Sun2,Marin", ""},
		{"Sun2,SHACKLER", "", "names Shackler, who holds numbers of record"},
		{" , ", "", "names no box"},
	} {
		boxes, boxesError := gateBoxes(row.setting)
		said := ""
		if boxesError != nil {
			said = boxesError.Error()
		}
		if strings.Join(boxes, ",") != row.boxes || (row.refusal == "") != (boxesError == nil) ||
			!strings.Contains(said, row.refusal) {
			test.Errorf("%q read as %v, %q; want %s, %q", row.setting, boxes, said, row.boxes, row.refusal)
		}
	}
	for setting, want := range map[string]time.Duration{"": 2 * time.Hour, "90m": 90 * time.Minute, "2s": 2 *
		time.Second, "never": 0, "0s": 0, "-1m": 0} {
		wait, waitError := gateWait(setting)
		if wait != want || (want == 0) != (waitError != nil) {
			test.Errorf("%s=%q read as %s, %v; want %s", gateWaitVariable, setting, wait, waitError, want)
		}
	}
}
