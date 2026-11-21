//go:build unix

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/executable_testing"
)

// trainTask is the task every test's train names, and unitTasks each fixture worktree's own, as its unit names it.
const trainTask = "trn0001"

var unitTasks = map[string]string{"a": "trnunta", "b": "trnuntb"}

// makeTrainUnit makes the fixture's worktree name a unit of its own task: its one commit, which adds <name>.txt, is
// made again with units/<name>/<name>.txt beside it, base.txt changed to base when base isn't empty, and the subject
// `base: add <name>.txt (#<its task>)`, as the house writes one.
func makeTrainUnit(fixture *landFixture, name string, base string) {
	fixture.test.Helper()
	worktree := fixture.worktree(name)
	files := map[string]string{filepath.Join("units", name, name+".txt"): name + "\n"}
	if base != "" {
		files["base.txt"] = base
	}
	for file, contents := range files {
		path := filepath.Join(worktree, file)
		if createDirectoryError := os.MkdirAll(filepath.Dir(path), 0o755); createDirectoryError != nil {
			fixture.test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(path, []byte(contents), 0o644); writeError != nil {
			fixture.test.Fatal(writeError)
		}
		fixture.git(worktree, "add", file)
	}
	fixture.git(worktree, "commit", "-q", "--amend", "-m", "base: add "+name+".txt (#"+unitTasks[name]+")")
}

// trainFixture is a land fixture whose worktrees a and b are units of tasks of their own, with a third worktree, c,
// at main, to land the train from, and a stand-in ahra that logs each command it is given. The train's pre-check is
// off, since the fixture's go is a stand-in. It returns the fixture and the stand-in's log.
func trainFixture(test *testing.T, baseOfA string, baseOfB string) (*landFixture, string) {
	test.Helper()
	fixture := newLandFixture(test)
	makeTrainUnit(fixture, "a", baseOfA)
	makeTrainUnit(fixture, "b", baseOfB)
	fixture.git(fixture.repository, "worktree", "add", "-q", "-b", "c", fixture.worktree("c"))
	ahraLog := filepath.Join(test.TempDir(), "ahra")
	standIn := filepath.Join(test.TempDir(), "ahra")
	if writeError := executable_testing.WriteFile(
		standIn,
		[]byte("#!/bin/sh\necho \"$*\" >> '"+ahraLog+"'\n"),
		0o755,
	); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, loomStandInVariable+"="+standIn,
		trainPreCheckVariable+"="+trainPreCheckOff)
	return fixture, ahraLog
}

// landTrainFrom runs `cohere-dev land` with arguments in the fixture's worktree c, and returns what it said and its
// exit code.
func landTrainFrom(fixture *landFixture, arguments ...string) (string, int) {
	fixture.test.Helper()
	landing := exec.Command(fixture.wrapper, append([]string{"land"}, arguments...)...)
	landing.Dir, landing.Env = fixture.worktree("c"), fixture.environment
	output, outputError := landing.CombinedOutput()
	var exited *exec.ExitError
	switch {
	case errors.As(outputError, &exited):
		return string(output), exited.ExitCode()
	case outputError != nil:
		fixture.test.Fatalf("running the train: %v\n%s", outputError, output)
	}
	return string(output), 0
}

// A train of two units lands as one merge onto main under one gate, its body naming each unit with its task, and
// closes each unit's task with the landing. The train's head is a chain of merges from main, one per unit in order.
func TestATrainOfTwoUnitsLandsUnderOneGate(test *testing.T) {
	test.Parallel()
	fixture, ahraLog := trainFixture(test, "", "")
	before := fixture.git(fixture.repository, "rev-parse", "main")
	unitA, unitB := fixture.git(fixture.repository, "rev-parse", "a"), fixture.git(fixture.repository, "rev-parse", "b")
	output, code := landTrainFrom(fixture, "--train", "a", "b", "--task", trainTask, "--message", "two units")
	if code != 0 {
		test.Fatalf("the train exited %d:\n%s", code, output)
	}

	added := strings.Fields(fixture.git(fixture.repository, "rev-list", "--first-parent", before+"..main"))
	if len(added) != 1 {
		test.Fatalf("main's first-parent line gained %d commits, want the train's one landing: %v", len(added), added)
	}
	landing := added[0]
	if files := fixture.git(fixture.repository, "ls-files"); !strings.Contains(files, "a.txt") ||
		!strings.Contains(files, "b.txt") {
		test.Errorf("main holds %q, want both units", files)
	}
	worktreeC := filepath.Base(fixture.worktree("c"))
	if gates := fixture.gates(); len(gates) != 2 || !strings.HasPrefix(gates[0], "start "+worktreeC+" ") ||
		gates[1] != "end "+worktreeC {
		test.Errorf("the gates ran %q, want one gate, in c", gates)
	}
	if subject := fixture.git(fixture.repository, "log", "-1", "--format=%s", landing); subject !=
		"Land #"+trainTask+": two units" {
		test.Errorf("the landing's subject is %q", subject)
	}
	body := fixture.git(fixture.repository, "log", "-1", "--format=%b", landing)
	for _, want := range []string{
		"A train of 2 units, gated once:\n\n- " + unitA[:8] + " #trnunta: base: add a.txt\n- " + unitB[:8] +
			" #trnuntb: base: add b.txt\n",
		"Gated by cohere-dev land in",
	} {
		if !strings.Contains(body, want) {
			test.Errorf("the landing's body lacks %q:\n%s", want, body)
		}
	}
	chain := fixture.git(fixture.repository, "log", "--first-parent", "--format=%s", before+".."+landing+"^2")
	if want := "Merge " + unitB[:8] + " (#trnuntb) into the train\nMerge " + unitA[:8] +
		" (#trnunta) into the train"; chain != want {
		test.Errorf("the train's own line reads\n%s\nwant\n%s", chain, want)
	}
	closed := strings.TrimSpace(string(readMaybeMissing(test, ahraLog)))
	if want := "tasks done trnunta Landed in " + landing[:8] + ", train #" + trainTask + ", gated once with " +
		"#trnuntb\ntasks done trnuntb Landed in " + landing[:8] + ", train #" + trainTask + ", gated once with " +
		"#trnunta"; closed != want {
		test.Errorf("ahra was told\n%s\nwant\n%s", closed, want)
	}
}

// A unit that lands on its own while the train waits in line is in the main the train joins: the train still lands what
// remains, says that unit landed alone, and closes only the other unit's task.
//
// a's gate holds until the train is waiting in line, so a lands while the train waits: a gate that ends on its own
// clock let a land before the train read main, and the train rightly refused a as held (Sun2, Nov 21).
func TestATrainLeavesAUnitThatLandedAloneClosedOnce(test *testing.T) {
	test.Parallel()
	fixture, ahraLog := trainFixture(test, "", "")
	release := filepath.Join(test.TempDir(), "release")
	stand := "#!/bin/sh\n[ \"$1\" = vet ] && exit 0\n" +
		"echo \"start $(basename \"$PWD\")\" >> '" + fixture.log + "'\n" +
		"while [ ! -e '" + release + "' ]; do sleep 0.05; done\n" +
		"echo \"end $(basename \"$PWD\")\" >> '" + fixture.log + "'\n"
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"), []byte(stand),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	alone := fixture.land("a")
	fixture.waitForGate("start " + filepath.Base(fixture.worktree("a")))
	said := filepath.Join(test.TempDir(), "train")
	file, createError := os.Create(said)
	if createError != nil {
		test.Fatal(createError)
	}
	train := exec.Command(fixture.wrapper, "land", "--train", "a", "b", "--task", trainTask, "--message", "two units")
	train.Dir, train.Env, train.Stdout, train.Stderr = fixture.worktree("c"), fixture.environment, file, file
	if startError := train.Start(); startError != nil {
		test.Fatal(startError)
	}
	// The child writes through its own copy of the descriptor, so this one closes once the child has it.
	if closeError := file.Close(); closeError != nil {
		test.Fatal(closeError)
	}
	for deadline := time.Now().Add(waiterBound); !strings.Contains(string(readMaybeMissing(test, said)),
		"waiting to land"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			test.Fatalf("the train never waited in line behind a:\n%s", readMaybeMissing(test, said))
		}
	}
	if writeError := os.WriteFile(release, nil, 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	if waitError := alone.Wait(); waitError != nil {
		test.Fatalf("a's own landing failed: %v", waitError)
	}
	trainError := train.Wait()
	output := string(readMaybeMissing(test, said))
	if trainError != nil {
		test.Fatalf("the train failed: %v\n%s", trainError, output)
	}
	if !strings.Contains(output, "#trnunta landed on its own while the train waited, so the train doesn't close it "+
		"again") {
		test.Errorf("the train never said a landed alone:\n%s", output)
	}
	closed := strings.TrimSpace(string(readMaybeMissing(test, ahraLog)))
	if strings.Contains(closed, "tasks done trnunta") || !strings.Contains(closed, "tasks done trnuntb") {
		test.Errorf("ahra was told\n%s\nwant b's task closed alone", closed)
	}
}

// A unit that clashes with the units before it is refused while the train is composed, before any gate, naming the
// unit, the units it clashes with, and the paths, and main doesn't move.
func TestATrainRefusesAUnitThatClashesBeforeTheGate(test *testing.T) {
	test.Parallel()
	fixture, ahraLog := trainFixture(test, "a's base\n", "b's base\n")
	before := fixture.git(fixture.repository, "rev-parse", "main")
	unitA, unitB := fixture.git(fixture.repository, "rev-parse", "a"), fixture.git(fixture.repository, "rev-parse", "b")
	output, code := landTrainFrom(fixture, "--train", "a", "b", "--task", trainTask)
	if want := "the train's unit 2, b at " + unitB[:8] + " (#trnuntb), clashes with the units before it, a at " +
		unitA[:8] + " (#trnunta)"; code != 2 || !strings.Contains(output, want) ||
		!strings.Contains(output, "base.txt") {
		test.Errorf("the clash ended with exit %d, want 2 naming %q and base.txt:\n%s", code, want, output)
	}
	if after := fixture.git(fixture.repository, "rev-parse", "main"); after != before {
		test.Errorf("main moved, %s to %s, on a refused train", before, after)
	}
	if gates := readMaybeMissing(test, fixture.log); len(gates) > 0 {
		test.Errorf("a gate ran before the refusal:\n%s", gates)
	}
	if closed := readMaybeMissing(test, ahraLog); len(closed) > 0 {
		test.Errorf("ahra was told to close a task:\n%s", closed)
	}
}

// A train is refused before the line, with no gate, when it names one head or five, a head main already holds, or no
// task of its own.
func TestATrainRefusesBeforeTheLine(test *testing.T) {
	test.Parallel()
	for _, refusal := range []struct {
		name      string
		arguments []string
		landFirst bool
		want      string
	}{
		{name: "one head", arguments: []string{"--train", "a", "--task", trainTask},
			want: "a train lands 2 to 4 heads, and 1 were given"},
		{name: "five heads", arguments: []string{"--train", "a", "b", "c", "main~0", "HEAD", "--task", trainTask},
			want: "a train lands 2 to 4 heads, and 5 were given"},
		{name: "a head main already holds", arguments: []string{"--train", "a", "b", "--task", trainTask},
			landFirst: true, want: "the train's unit a is nothing to land, and no gate runs: main already holds"},
		{name: "no task", arguments: []string{"--train", "a", "b"},
			want: "a train names a task of its own with --task <id>"},
	} {
		test.Run(refusal.name, func(test *testing.T) {
			test.Parallel()
			fixture, ahraLog := trainFixture(test, "", "")
			gatesBefore := 0
			if refusal.landFirst {
				if landError := fixture.land("a").Wait(); landError != nil {
					test.Fatalf("a's own landing failed: %v", landError)
				}
				gatesBefore = len(fixture.gates())
			}
			before := fixture.git(fixture.repository, "rev-parse", "main")
			output, code := landTrainFrom(fixture, refusal.arguments...)
			if code != 2 || !strings.Contains(output, refusal.want) {
				test.Errorf("the train ended with exit %d, want 2 saying %q:\n%s", code, refusal.want, output)
			}
			if after := fixture.git(fixture.repository, "rev-parse", "main"); after != before {
				test.Errorf("main moved, %s to %s, on a refused train", before, after)
			}
			if gates := strings.TrimSpace(string(readMaybeMissing(test, fixture.log))); gates != "" &&
				len(strings.Split(gates, "\n")) != gatesBefore {
				test.Errorf("a gate ran for the refused train:\n%s", gates)
			}
			if closed := readMaybeMissing(test, ahraLog); len(closed) > 0 {
				test.Errorf("ahra was told to close a task:\n%s", closed)
			}
		})
	}
}

// A red pre-check refuses the train before the line, exit 1, naming what failed, and puts the worktree back on its
// branch, with no gate. The stand-in go fails `go build`, so the pre-check is red at its first step.
func TestATrainWhosePreCheckIsRedNeverTakesItsPlace(test *testing.T) {
	test.Parallel()
	fixture, ahraLog := trainFixture(test, "", "")
	// The pre-check on: a later entry wins.
	fixture.environment = append(fixture.environment, trainPreCheckVariable+"=")
	bin := test.TempDir()
	failing := "#!/bin/sh\n[ \"$1\" = build ] && { echo 'planted build failure'; exit 1; }\nexec '" +
		filepath.Join(filepath.Dir(fixture.log), "go") + "' \"$@\"\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "go"), []byte(failing), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	before := fixture.git(fixture.repository, "rev-parse", "main")
	output, code := landTrainFrom(fixture, "--train", "a", "b", "--task", trainTask)
	if code != 1 || !strings.Contains(output, "the train's pre-check is red at go build ./..., so it never took a "+
		"place in the land line") || !strings.Contains(output, "planted build failure") {
		test.Errorf("the red pre-check ended with exit %d, want 1 naming go build:\n%s", code, output)
	}
	if branch := fixture.git(fixture.worktree("c"), "symbolic-ref", "--short", "HEAD"); branch != "c" {
		test.Errorf("c is on %q after the pre-check, want back on c", branch)
	}
	if after := fixture.git(fixture.repository, "rev-parse", "main"); after != before {
		test.Errorf("main moved, %s to %s, on a refused train", before, after)
	}
	if gates := readMaybeMissing(test, fixture.log); len(gates) > 0 {
		test.Errorf("a gate ran after a red pre-check:\n%s", gates)
	}
	if closed := readMaybeMissing(test, ahraLog); len(closed) > 0 {
		test.Errorf("ahra was told to close a task:\n%s", closed)
	}
}

// A red gate lands nothing, puts the worktree back, closes no task, and names the directories each unit's diff
// touches, saying it can't tell whose unit failed. The stand-in go fails the gate's `go test`.
func TestATrainsRedGateNamesEachUnitsDirectories(test *testing.T) {
	test.Parallel()
	fixture, ahraLog := trainFixture(test, "", "")
	bin := test.TempDir()
	failing := "#!/bin/sh\n[ \"$1\" = test ] && { echo 'planted test failure'; exit 1; }\nexec '" +
		filepath.Join(filepath.Dir(fixture.log), "go") + "' \"$@\"\n"
	if writeError := executable_testing.WriteFile(filepath.Join(bin, "go"), []byte(failing), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	before := fixture.git(fixture.repository, "rev-parse", "main")
	unitA, unitB := fixture.git(fixture.repository, "rev-parse", "a"), fixture.git(fixture.repository, "rev-parse", "b")
	output, code := landTrainFrom(fixture, "--train", "a", "b", "--task", trainTask)
	for _, want := range []string{
		"the gate failed, so nothing landed",
		"can't say whose unit they are",
		"  a at " + unitA[:8] + " (#trnunta): ., units/a\n",
		"  b at " + unitB[:8] + " (#trnuntb): ., units/b\n",
	} {
		if !strings.Contains(output, want) {
			test.Errorf("the red gate's account lacks %q:\n%s", want, output)
		}
	}
	if code == 0 {
		test.Errorf("a train with a red gate exited 0:\n%s", output)
	}
	if after := fixture.git(fixture.repository, "rev-parse", "main"); after != before {
		test.Errorf("main moved, %s to %s, on a red gate", before, after)
	}
	if branch := fixture.git(fixture.worktree("c"), "symbolic-ref", "--short", "HEAD"); branch != "c" {
		test.Errorf("c is on %q after the red gate, want back on c", branch)
	}
	if closed := readMaybeMissing(test, ahraLog); len(closed) > 0 {
		test.Errorf("ahra was told to close a task:\n%s", closed)
	}
}
