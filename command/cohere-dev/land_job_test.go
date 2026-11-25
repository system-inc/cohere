//go:build unix

package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// landJobFixture is a land job ready to judge a merge in a mirror: a candidate, worktree a's head, bringing a module
// with one package and a command/cohere, so there is a cohere to measure; the mirror, a bare repository the merge is
// put in; the job's own tree, a directory with no history; go stood in for by one that records each call and lists
// the module's packages; cohere on the tree by a clean run; and bash, for the land job's two scripts, by landJobShell,
// answering from replies.
type landJobFixture struct {
	*landFixture
	replies string
	goCalls string
	home    string
	mirror  string
	job     string
}

// landJobShell stands in for bash in the land job's scripts. It keeps the script it was handed in `<kind>.script` and
// its arguments, one a line, in `<kind>.arguments`, the kind `measure` for performanceScript and `tests` for
// macBoxScript, and writes each kind to `calls`. It prints `<kind>.run`, `COMMIT` the commit it was handed, and exits
// `<kind>.exit`; with no `tests.run` it prints a passing run of every package it was handed, but `tests.fails`, which
// fails, and then, unless one failed, a passing -race run of each race token's package.
const landJobShell = `#!/bin/sh
replies="$LAND_JOB_REPLIES"
staged="$replies/stdin.$$"
cat > "$staged"
if grep -q '` + performanceMarker + `' "$staged"; then kind=measure; else kind=tests; fi
mv "$staged" "$replies/$kind.script"
shift 2
printf '%s\n' "$@" > "$replies/$kind.arguments"
echo "$kind" >> "$replies/calls"
commit="$1"
if [ -e "$replies/$kind.run" ]; then
  sed "s/COMMIT/$commit/g" "$replies/$kind.run"
  exit "$(cat "$replies/$kind.exit" 2>/dev/null || echo 0)"
fi
[ "$kind" = tests ] || exit 1
fails=$(cat "$replies/tests.fails" 2>/dev/null || true)
echo "` + boxMarker + `$commit on $LAND_JOB_PLATFORM"
races="$3"
shift 3
failed=0
for package in "$@"; do
  if [ "$package" = "$fails" ]; then
    printf '%s\n' '{"Action":"output","Package":"'"$package"'","Test":"TestPlantedFailure",'\
'"Output":"planted: it fails\n"}'
    echo '{"Action":"fail","Package":"'"$package"'","Test":"TestPlantedFailure"}'
    echo '{"Action":"fail","Package":"'"$package"'","Elapsed":1}'; failed=1; continue
  fi
  echo '{"Action":"pass","Package":"'"$package"'","Elapsed":1}'
done
[ "$failed" = 0 ] || exit 1
[ -n "$races" ] || exit 0
echo "` + boxRaceMarker + `"
for token in $(printf '%s' "$races" | tr , ' '); do
  directory="${token%%@*}"
  echo '{"Action":"pass","Package":"example.com/fixture/'"${directory#./}"'","Elapsed":1}'
done
`

func newLandJobFixture(test *testing.T) *landJobFixture {
	test.Helper()
	fixture := &landJobFixture{landFixture: newLandFixture(test), replies: test.TempDir(), home: test.TempDir(),
		mirror: filepath.Join(test.TempDir(), "cohere.git"), job: test.TempDir()}
	stand := test.TempDir()
	fixture.goCalls = filepath.Join(stand, "go-calls")
	// go records each call, lists the module's packages, and runs the docs data check, failing it when asked.
	goStandIn := "#!/bin/sh\necho \"$*\" >> '" + fixture.goCalls + "'\n" +
		"if [ \"$1\" = run ]; then [ -z \"$LAND_JOB_DOCS_FAILS\" ] || exit 1; exit 0; fi\n" + macBoxGoList
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"), []byte(goStandIn),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	shell := filepath.Join(stand, "shell")
	if writeError := executable_testing.WriteFile(shell, []byte(landJobShell), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	cohere := filepath.Join(stand, "cohere")
	if writeError := executable_testing.WriteFile(cohere, []byte("#!/bin/sh\necho '"+performanceCleanSummary+"'\n"),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, "HOME="+fixture.home, landJobShellVariable+"="+shell,
		"LAND_JOB_REPLIES="+fixture.replies, "LAND_JOB_PLATFORM="+runtime.GOOS+"/"+runtime.GOARCH,
		gateCohereVariable+"="+cohere, landJobMirrorVariable+"="+fixture.mirror)
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "go.mod"), "module example.com/fixture\n\ngo 1.27\n")
	writeFixtureFile(test, filepath.Join(worktree, "alpha", "alpha_test.go"),
		"package alpha\n\nimport \"testing\"\n\nfunc TestAlpha(test *testing.T) {}\n")
	writeFixtureFile(test, filepath.Join(worktree, "command", "cohere", "main.go"), "package main\n\nfunc main() {}\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a module with a cohere"))
	fixture.git(fixture.repository, "init", "-q", "--bare", fixture.mirror)
	return fixture
}

// merged makes the merge Loom makes, of worktree a's head onto main, puts it in the mirror with main and the candidate,
// and returns it.
func (fixture *landJobFixture) merged() string {
	fixture.test.Helper()
	candidate := fixture.git(fixture.worktree("a"), "rev-parse", "HEAD")
	tree := strings.Fields(fixture.git(fixture.repository, "merge-tree", "--write-tree", "main", candidate))[0]
	merge := fixture.git(fixture.repository, "commit-tree", tree, "-p", "main", "-p", candidate, "-m",
		"Merge a onto main")
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, "main:refs/heads/main",
		merge+":refs/heads/merge")
	return merge
}

// reply writes what the stand-in bash answers from file in the replies directory.
func (fixture *landJobFixture) reply(file string, contents string) {
	fixture.test.Helper()
	writeFixtureFile(fixture.test, filepath.Join(fixture.replies, file), contents)
}

// landJobRan is what a land job did: its exit, what it printed to its output and its errors, and the tree it named on
// its last line, empty when it named none.
type landJobRan struct {
	exit     int
	output   string
	errors   string
	landed   string
	lastLine string
}

// landJob runs land-job on merge in the job's own tree, which has no history, as Loom runs it, at main unless
// environment names another commit.
func (fixture *landJobFixture) landJob(merge string, environment ...string) landJobRan {
	fixture.test.Helper()
	job := exec.Command(fixture.wrapper, "land-job", "--merge", merge, "--task", fixtureTask, "--message",
		"a module measured and gated")
	job.Dir = fixture.job
	job.Env = append(append(fixture.environment, landJobCommitVariable+"="+fixture.git(fixture.repository,
		"rev-parse", "main")), environment...)
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	job.Stdout, job.Stderr = &output, &errorOutput
	ran := landJobRan{}
	if runError := job.Run(); runError != nil {
		var exited *exec.ExitError
		if !errors.As(runError, &exited) {
			fixture.test.Fatal(runError)
		}
		ran.exit = exited.ExitCode()
	}
	ran.output, ran.errors = output.String(), errorOutput.String()
	lines := strings.Split(strings.TrimSpace(ran.output), "\n")
	ran.lastLine = lines[len(lines)-1]
	if landed, found := strings.CutPrefix(ran.lastLine, "landed "); found {
		ran.landed = landed
	}
	return ran
}

// arguments is what the stand-in bash was handed for kind, one a line, nil when that script never ran.
func (fixture *landJobFixture) arguments(kind string) []string {
	fixture.test.Helper()
	said := strings.TrimSuffix(string(readMaybeMissing(fixture.test, filepath.Join(fixture.replies,
		kind+".arguments"))), "\n")
	if said == "" {
		return nil
	}
	return strings.Split(said, "\n")
}

// landingSaid is what a land job's last lines say, as Loom's finisher reads them: the performance file, nil when
// unchanged, the tree, and the message.
type landingSaid struct {
	performance []byte
	unchanged   bool
	tree        string
	message     string
}

// readLanding reads a land job's output as Loom's finisher does, failing the test when its lines aren't in order.
func readLanding(test *testing.T, output string) landingSaid {
	test.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	begin := slices.Index(lines, landJobMarker+"message begin")
	end := slices.Index(lines, landJobMarker+"message end")
	if begin != 2 || end <= begin || end != len(lines)-2 {
		test.Fatalf("the landing's lines are out of order:\n%s", output)
	}
	said := landingSaid{message: strings.Join(lines[begin+1:end], "\n") + "\n"}
	performance, found := strings.CutPrefix(lines[0], landJobMarker+"performance ")
	tree, treeFound := strings.CutPrefix(lines[1], landJobMarker+"tree ")
	if !found || !treeFound || lines[len(lines)-1] != "landed "+tree {
		test.Fatalf("the landing's lines don't read:\n%s", output)
	}
	said.tree = tree
	if performance == "unchanged" {
		said.unchanged = true
		return said
	}
	contents, decodeError := base64.StdEncoding.DecodeString(performance)
	if decodeError != nil {
		test.Fatalf("the performance line isn't base64: %v", decodeError)
	}
	said.performance = contents
	return said
}

// rebuilt is the tree Loom's finisher makes from what the land job said: the merge's tree, with the performance file
// written when it changed.
func (fixture *landJobFixture) rebuilt(merge string, said landingSaid) string {
	fixture.test.Helper()
	tree := fixture.git(fixture.repository, "rev-parse", merge+"^{tree}")
	if said.unchanged {
		return tree
	}
	rebuilt, spliceError := spliceIntoTree(fixture.repository, tree, performanceFileName, said.performance)
	if spliceError != nil {
		fixture.test.Fatal(spliceError)
	}
	return rebuilt
}

// A merge whose candidate is clean would land (#etpnnw1, #hnbb5vr): measured first, on a script handed no binary of
// main's since main holds no performance file, then tested, then checked, all on the merge's tree laid from the mirror.
// Nothing is committed or pushed: the last lines are the performance file, the tested tree, which the finisher
// rebuilds from the merge and that file to the same hash, and the landing's message, `Land #<task>: <what>` with the
// gate's account, the measurement's note and every step's seconds, then `landed <tree>`. With COHERE_LAND_CACHE set
// the tests' script keeps the land job's cache, and unset it keeps the gate box's none; either way it reads the land
// job's own mirrors.
func TestALandJobPassesAMergeAndSaysItsLanding(test *testing.T) {
	test.Parallel()
	for _, kept := range []bool{false, true} {
		test.Run(map[bool]string{false: "no kept cache", true: "a kept cache"}[kept], func(test *testing.T) {
			test.Parallel()
			fixture := newLandJobFixture(test)
			fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
			merge := fixture.merged()
			environment := []string{}
			cache := filepath.Join(test.TempDir(), "land cache")
			if kept {
				environment = append(environment, landJobCacheVariable+"="+cache)
			}
			ran := fixture.landJob(merge, environment...)
			if ran.exit != 0 || ran.landed == "" {
				test.Fatalf("the land job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
			}
			if calls := string(readMaybeMissing(test, filepath.Join(fixture.replies, "calls"))); calls !=
				"measure\ntests\n" {
				test.Errorf("the scripts ran %q, want the measurement, then the tests", calls)
			}
			if measured := fixture.arguments("measure"); len(measured) != 6 || measured[0] != merge ||
				measured[1] != performanceReference {
				test.Errorf("the measurement was handed %q, want the merge, deeafbf4 and the rounds, and no binary "+
					"of main's", measured)
			}
			tested := fixture.arguments("tests")
			if len(tested) < 4 || tested[0] != merge || tested[1] != "" ||
				!slices.Contains(tested[3:], "example.com/fixture/alpha") {
				test.Errorf("the tests' script was handed %q, want the merge, no pin, and the packages the laid tree "+
					"lists", tested)
			}
			script := string(readMaybeMissing(test, filepath.Join(fixture.replies, "tests.script")))
			mirrors := filepath.Join(fixture.home, landJobScratch, "mirrors")
			wantCache := macBoxLandCacheLine
			if kept {
				wantCache = "landCache='" + cache + "'"
			}
			if !strings.Contains(script, "\nmirrors='"+mirrors+"'; tries=1; pause=0\n"+wantCache+"\n") ||
				!strings.HasSuffix(script, sharedBoxGate) {
				test.Errorf("the tests' script doesn't read the land job's mirrors with %s, or isn't the gate box's:"+
					"\n%.600s", wantCache, script)
			}
			said := readLanding(test, ran.output)
			if said.unchanged || !bytes.HasPrefix(said.performance, []byte(performanceHeader)) {
				test.Errorf("the performance line isn't a measured file:\n%s", said.performance)
			}
			if rebuilt := fixture.rebuilt(merge, said); rebuilt != said.tree {
				test.Errorf("the finisher rebuilds %s from the lines, and the land job said %s", rebuilt, said.tree)
			}
			for _, want := range []string{
				"Land #" + fixtureTask + ": a module measured and gated\n\n",
				"Gated by cohere-dev land-job on Shackler in ",
				": go test of the module on Shackler (" + runtime.GOOS + "/" + runtime.GOARCH + ", 2 packages, " +
					"shared uid, ",
				", go vet, cohere on the tree, 0 findings and no gaps.",
				"Measured on Shackler for " + performanceFileName + ": the first measurement, so nothing to compare.",
				"Steps: preflight ",
			} {
				if !strings.Contains(said.message, want) {
					test.Errorf("the landing's message lacks %q:\n%s", want, said.message)
				}
			}
			for _, step := range []string{"preflight", "lay", "measure", "tests", "checks"} {
				if !strings.Contains(said.message, ", "+step+" ") &&
					!strings.Contains(said.message, "Steps: "+step+" ") {
					test.Errorf("the landing's message doesn't say the %s step's seconds:\n%s", step, said.message)
				}
			}
		})
	}
}

// A merge that builds main's binary keeps main's file (#6ex523f): the script is handed main's binary's sha256, says it
// built that very binary and stops before any run, and the land job says the performance file is unchanged, the
// tested tree the merge's own, and that it is the same binary as main's, so not measured.
func TestALandJobKeepsMainsFileForTheSameBinary(test *testing.T) {
	test.Parallel()
	fixture := newLandJobFixture(test)
	previous := sameBinaryPerformance(test, previousPerformance(test, performancePreviousCommit, performanceBox,
		performanceSpreadOf(9850, 9600, 10000), performanceSpreadOf(39000, 38000, 40000)))
	writeFixtureFile(test, filepath.Join(fixture.repository, performanceFileName), string(previous))
	fixture.git(fixture.repository, "add", performanceFileName)
	fixture.git(fixture.repository, "commit", "-q", "-m", "main's measurement before")
	fixture.reply("measure.run", performanceMarker+"tested COMMIT on darwin/arm64\n"+performanceMarker+"binary "+
		performanceFixtureBinary+"\n"+performanceSameBinaryMarker+"\n")
	merge := fixture.merged()
	ran := fixture.landJob(merge)
	if ran.exit != 0 || ran.landed == "" {
		test.Fatalf("the land job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	if measured := fixture.arguments("measure"); len(measured) != 7 || measured[6] != performanceFixtureBinary {
		test.Errorf("the measurement was handed %q, want main's binary last", measured)
	}
	said := readLanding(test, ran.output)
	if !said.unchanged || said.tree != fixture.git(fixture.repository, "rev-parse", merge+"^{tree}") {
		test.Errorf("the land job said the file changed, or a tree %s other than the merge's", said.tree)
	}
	if want := "the same binary as main's (sha256 " + performanceFixtureBinary[:12] + "), so not measured"; !strings.
		Contains(said.message, want) {
		test.Errorf("the landing's message doesn't say %q:\n%s", want, said.message)
	}
}

// A red test refuses: the land job names the test and its output, exits 1, and says no landing.
func TestALandJobRefusesARedTestNamingIt(test *testing.T) {
	test.Parallel()
	fixture := newLandJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	fixture.reply("tests.fails", "example.com/fixture/alpha")
	ran := fixture.landJob(fixture.merged())
	if ran.exit != 1 || ran.landed != "" || strings.Contains(ran.output, landJobMarker) {
		test.Fatalf("the land job exited %d and said %q, want 1 and no landing:\n%s", ran.exit, ran.output,
			ran.errors)
	}
	for _, want := range []string{"Shackler's tests on", "failed, so nothing landed",
		"example.com/fixture/alpha TestPlantedFailure", "planted: it fails", "land-job step tests refused in ",
		"land-job refused after "} {
		if !strings.Contains(ran.errors, want) {
			test.Errorf("the land job doesn't say %q:\n%s", want, ran.errors)
		}
	}
}

// A merge whose first parent isn't the commit the job runs at refuses before anything is laid, measured or tested:
// its code wouldn't be the main it judges.
func TestALandJobRefusesAMergeOntoAnotherMain(test *testing.T) {
	test.Parallel()
	fixture := newLandJobFixture(test)
	merge := fixture.merged()
	elsewhere := fixture.git(fixture.worktree("b"), "rev-parse", "HEAD")
	ran := fixture.landJob(merge, landJobCommitVariable+"="+elsewhere)
	if ran.exit != 1 || ran.landed != "" {
		test.Fatalf("the land job exited %d, its last line %q, want 1 and no landing:\n%s", ran.exit, ran.lastLine,
			ran.errors)
	}
	if want := "and this job runs at " + shortCommit(elsewhere); !strings.Contains(ran.errors, want) {
		test.Errorf("the land job doesn't say %q:\n%s", want, ran.errors)
	}
	if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
		test.Errorf("a merge onto another main ran %q", calls)
	}
}

// The gate's Go cache by landCache (#s50bcnr), sharedBoxGate run for real with bash against a planted toolchain:
// unset, every go test carries -count=1 on a cache made empty for the job and gone after it, as on every gate box; set,
// no go test carries it, the cache is landCache's gocache, and it stays for the next job. No go test in the gate's
// scripts spells -count=1 itself, so the one setting is what every go test reads.
func TestTheLandJobsCacheIsKeptAndDropsCountOne(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate is a bash script")
	needs.Tool(test, "sha256sum", "the gate digests the toolchain with it, as the boxes do")
	if count := strings.Count(sharedBoxGate, "-count=1"); count != 1 || strings.Contains(sharedBoxShards, "-count=1") {
		test.Errorf("the gate's scripts spell -count=1 %d times, want once, in the cache's own setting", count)
	}
	goroot := filepath.Join(test.TempDir(), "go")
	writeFixtureFile(test, filepath.Join(goroot, "src", "fmt", "print.go"), "package fmt\n")
	if makeError := os.MkdirAll(filepath.Join(goroot, "bin"), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := executable_testing.WriteFile(filepath.Join(goroot, "bin", "go"), []byte(plantedGo),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	digest := toolchainDigest(test, goroot)
	for _, kept := range []bool{false, true} {
		test.Run(map[bool]string{false: "unset", true: "set"}[kept], func(test *testing.T) {
			test.Parallel()
			tree := test.TempDir()
			writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
			log := filepath.Join(test.TempDir(), "asked")
			temporary := test.TempDir()
			landCache := ""
			if kept {
				landCache = filepath.Join(test.TempDir(), "land cache")
			}
			script := "set -euo pipefail\n" +
				`goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; landCache="$6"; shift 6` + "\n" +
				sharedBoxGate
			gate := exec.Command("bash", "-c", script, "gate", goroot, digest, filepath.Join(tree, "no-node"),
				strings.Repeat("0", 64), "", landCache, "./...")
			gate.Dir = tree
			gate.Env = append(os.Environ(), "GATE_LOG="+log, "TMPDIR="+temporary, "GOCACHE="+test.TempDir())
			if output, gateError := gate.CombinedOutput(); gateError != nil {
				test.Fatalf("the gate failed: %v\n%s", gateError, output)
			}
			asked := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, log))), "\n")
			if len(asked) != 3 || !strings.HasPrefix(asked[2], "test test ") {
				test.Fatalf("the gate asked for %q, want go mod download, go mod verify and the module's tests", asked)
			}
			cache := filepath.Join(landCache, "gocache")
			if !kept {
				entries := listMaybeMissing(test, temporary)
				if len(entries) != 0 {
					test.Errorf("the gate left its empty cache behind: %v", entries)
				}
				if !strings.Contains(asked[2], "test test -count=1 -timeout ") ||
					!strings.Contains(asked[2], "| cache "+temporary+"/cohere-gate-gocache.") ||
					!strings.Contains(asked[2], " empty yes ") {
					test.Errorf("unset, the tests ran %q, want -count=1 on an empty cache of the job's own", asked[2])
				}
				return
			}
			if strings.Contains(asked[2], "-count") || !strings.Contains(asked[2], "| cache "+cache+" empty yes ") {
				test.Errorf("set, the tests ran %q, want no -count on the land job's cache %s", asked[2], cache)
			}
			if information, statError := os.Stat(cache); statError != nil || !information.IsDir() {
				test.Errorf("the land job's cache didn't stay after the gate: %v", statError)
			}
		})
	}
}

// The tests' temporary directory on a gate Mac, macBoxTemporaryBlock run for real with bash: a new one each run, while
// with landCache set it is one path each run, emptied first, so go test's cache, which keys a test on the `TMPDIR` it
// read, replays across land jobs.
func TestTheLandJobsTemporaryDirectoryIsOnePath(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
	// The paths under /tmp the block names, put under the test's own directory so no job's own is touched.
	under := test.TempDir()
	block := strings.NewReplacer("/tmp/cohere-land-gate.", under+"/cohere-land-gate.", "/tmp/cohere-gate.",
		under+"/cohere-gate.").Replace(macBoxTemporaryBlock)
	if block == macBoxTemporaryBlock {
		test.Fatal("the block's paths were not found to replace")
	}
	run := func(landCache string) string {
		test.Helper()
		bash := exec.Command("bash", "-s")
		bash.Stdin = strings.NewReader("set -euo pipefail\nrefuse() { echo \"refused $*\"; exit 1; }\nlandCache=" +
			shellQuoted(landCache) + "\n" + block + "touch \"$gatetemp/left\"\necho \"$gatetemp\"\n")
		output, bashError := bash.CombinedOutput()
		if bashError != nil {
			test.Fatalf("the block failed: %v\n%s", bashError, output)
		}
		return strings.TrimSpace(string(output))
	}
	first, second := run("/kept"), run("/kept")
	if first != second || !strings.HasPrefix(first, under+"/cohere-land-gate.") {
		test.Errorf("with a kept cache the runs' temporary directories are %s and %s, want one path", first, second)
	}
	if entries := listMaybeMissing(test, second); len(entries) != 1 {
		test.Errorf("the second run found %d entries in its temporary directory, want only its own", len(entries))
	}
	if cold, again := run(""), run(""); cold == again || !strings.HasPrefix(cold, under+"/cohere-gate.") {
		test.Errorf("without a kept cache the runs' temporary directories are %s and %s, want a new one each", cold,
			again)
	}
}

// The land job's mirrors, read by macBoxScript run for real with bash: a merge held only in the cohere mirror, and its
// TypeScript pin held only in the TypeScript mirror beside it, are fetched through the land job's borrowing mirrors and
// checked out in the scratch tree, nothing copied, before the script reaches sharedBoxGate, which here finds no
// toolchain.
func TestTheLandJobsMirrorsHoldTheMergeAndItsPin(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
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
	// The house's mirrors: TypeScript's, holding the pin, and cohere's, holding a merge that records it.
	house := filepath.Join(directory, "house")
	source := filepath.Join(directory, "TypeScript")
	writeFixtureFile(test, filepath.Join(source, "README.md"), "TypeScript\n")
	git(directory, "init", "-q", "-b", "main", source)
	git(source, "add", ".")
	git(source, "commit", "-q", "-m", "TypeScript")
	pin := git(source, "rev-parse", "HEAD")
	git(directory, "init", "-q", "--bare", filepath.Join(house, "TypeScript.git"))
	git(source, "push", "-q", filepath.Join(house, "TypeScript.git"), "HEAD:refs/heads/main")
	checkout := filepath.Join(directory, "checkout")
	writeFixtureFile(test, filepath.Join(checkout, "go.mod"), "module example.com/module\n\ngo 1.27\n")
	git(directory, "init", "-q", "-b", "main", checkout)
	git(checkout, "add", ".")
	git(checkout, "update-index", "--add", "--cacheinfo", "160000,"+pin+",TypeScript")
	git(checkout, "commit", "-q", "-m", "main")
	merge := git(checkout, "commit-tree", "-p", "HEAD", "-m", "the merge", git(checkout, "rev-parse", "HEAD^{tree}"))
	git(directory, "init", "-q", "--bare", filepath.Join(house, "cohere.git"))
	git(checkout, "push", "-q", filepath.Join(house, "cohere.git"), merge+":refs/heads/merge")
	mirrors := filepath.Join(directory, "home", landJobScratch, "mirrors")
	for range 2 {
		// Laid again, as the next job lays them, they are kept and borrow anew.
		if layError := layLandJobMirrors(filepath.Join(house, "cohere.git", "objects"), mirrors,
			[]string{filepath.Join(house, "TypeScript.git", "objects")}); layError != nil {
			test.Fatal(layError)
		}
	}
	script, scriptError := withLandJobMirrors(macBoxScript, mirrors)
	if scriptError != nil {
		test.Fatal(scriptError)
	}
	noGo := filepath.Join(directory, "no-go")
	script = strings.Replace(script, `goroot="`+macBoxGoroot+`"`, `goroot="`+noGo+`"`, 1)
	home := filepath.Join(directory, "home")
	bash := exec.Command("bash", "-s", "--", merge, pin, "", "./...")
	bash.Dir, bash.Stdin = test.TempDir(), strings.NewReader(script)
	bash.Env = append(environment, "HOME="+home)
	output, bashError := bash.CombinedOutput()
	if bashError == nil || !strings.Contains(string(output), sharedBoxRefusedMarker+"there is no go at "+noGo) {
		test.Fatalf("the script didn't reach the toolchain check (%v):\n%s", bashError, output)
	}
	tree := filepath.Join(home, strings.TrimPrefix(macBoxScratch, "~/"), "tree")
	if at := git(tree, "rev-parse", "HEAD"); at != merge {
		test.Errorf("the scratch tree is at %s, want the merge %s", at, merge)
	}
	if at := git(filepath.Join(tree, "TypeScript"), "rev-parse", "HEAD"); at != pin {
		test.Errorf("the scratch tree's TypeScript is at %s, want the pin %s", at, pin)
	}
	if objects := git(filepath.Join(mirrors, "cohere.git"), "count-objects", "-v"); !strings.Contains(objects,
		"count: 0\n") || !strings.Contains(objects, "in-pack: 0\n") {
		test.Errorf("the land job's cohere mirror copied objects:\n%s", objects)
	}
}
