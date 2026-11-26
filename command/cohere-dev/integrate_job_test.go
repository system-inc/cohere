//go:build unix

package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/testing/executable_testing"
	"github.com/system-inc/cohere/internal/testing/needs"
)

// integrateJobFixture is an integrate job ready to judge a merge in a mirror: a candidate, worktree a's head, bringing
// a module with one package and a command/cohere, so there is a cohere to measure; the mirror, a bare repository the
// merge is put in; the job's own tree, a directory with no history; go stood in for by one that records each call and
// lists the module's packages; cohere on the tree by a clean run, keeping its arguments one a line in cohereArguments;
// bash, for the integrate job's two scripts, by integrateJobShell, answering from replies; and the integrate job's git
// by gitStandIn, in a directory whose name a shell has to quote, which records each call in gitCalls and runs the
// test's own git.
type integrateJobFixture struct {
	*landFixture
	replies    string
	goCalls    string
	gitStandIn string
	gitCalls   string
	// cohereArguments is what cohere on the tree was handed, one a line.
	cohereArguments string
	home            string
	mirror          string
	job             string
}

// integrateJobShell stands in for bash in the integrate job's scripts. It keeps the script it was handed in
// `<kind>.script` and its arguments, one a line, in `<kind>.arguments`, the kind `measure` for performanceScript and
// `tests` for macBoxScript, writes each kind to `calls`, and keeps the setting of needs.SwiftUnlaidVariable it ran
// under in `<kind>.swift`. It prints `<kind>.run`, `COMMIT` the commit it was handed, and exits `<kind>.exit`; with no
// `tests.run` it prints where its modules came from, as an integrate job's gate says it, then a passing run of every
// package it was handed, but `tests.fails`, which fails, with `tests.cached`'s package replayed `(cached)`, and then,
// unless one failed, a passing -race run of each race token's package. With `tests.grows` it writes an entry at the
// path it names, as a run that compiles grows the cache.
const integrateJobShell = `#!/bin/sh
replies="$INTEGRATE_JOB_REPLIES"
staged="$replies/stdin.$$"
cat > "$staged"
if grep -q '` + performanceMarker + `' "$staged"; then kind=measure; else kind=tests; fi
mv "$staged" "$replies/$kind.script"
shift 2
printf '%s\n' "$@" > "$replies/$kind.arguments"
echo "$kind" >> "$replies/calls"
printf '%s' "${` + needs.SwiftUnlaidVariable + `:-}" > "$replies/$kind.swift"
commit="$1"
if [ -e "$replies/$kind.run" ]; then
  sed "s/COMMIT/$commit/g" "$replies/$kind.run"
  exit "$(cat "$replies/$kind.exit" 2>/dev/null || echo 0)"
fi
[ "$kind" = tests ] || exit 1
fails=$(cat "$replies/tests.fails" 2>/dev/null || true)
echo "` + boxModulesMarker + `the box's laid set at /opt/loom-tools/gomod-cohere, offline"
cached=$(cat "$replies/tests.cached" 2>/dev/null || true)
[ ! -e "$replies/tests.grows" ] || echo entry > "$(cat "$replies/tests.grows")"
echo "` + boxMarker + `$commit on $INTEGRATE_JOB_PLATFORM"
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
  [ "$package" != "$cached" ] ||
    printf '%s\n' '{"Action":"output","Package":"'"$package"'","Output":"ok  \t'"$package"'\t(cached)\n"}'
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

func newIntegrateJobFixture(test *testing.T) *integrateJobFixture {
	test.Helper()
	fixture := &integrateJobFixture{landFixture: newLandFixture(test), replies: test.TempDir(), home: test.TempDir(),
		mirror: filepath.Join(test.TempDir(), "cohere.git"), job: test.TempDir()}
	stand := test.TempDir()
	fixture.goCalls = filepath.Join(stand, "go-calls")
	// go records each call with the proxy and the checksum database it ran under, lists the module's packages, and runs
	// the docs data check, failing it when asked.
	goStandIn := "#!/bin/sh\necho \"$* | proxy ${GOPROXY:-} | sum database ${GOSUMDB:-}\" >> '" + fixture.goCalls +
		"'\n" +
		"if [ \"$1\" = run ]; then [ -z \"$INTEGRATE_JOB_DOCS_FAILS\" ] || exit 1; exit 0; fi\n" + macBoxGoList
	if writeError := executable_testing.WriteFile(filepath.Join(filepath.Dir(fixture.log), "go"), []byte(goStandIn),
		0o755); writeError != nil {
		test.Fatal(writeError)
	}
	shell := filepath.Join(stand, "shell")
	if writeError := executable_testing.WriteFile(shell, []byte(integrateJobShell), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	cohere := filepath.Join(stand, "cohere")
	fixture.cohereArguments = filepath.Join(stand, "cohere-arguments")
	if writeError := executable_testing.WriteFile(cohere, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > "+
		shellQuoted(fixture.cohereArguments)+"\necho '"+performanceCleanSummary+"'\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	testGit, lookError := exec.LookPath("git")
	if lookError != nil {
		test.Fatal(lookError)
	}
	fixture.gitCalls = filepath.Join(stand, "git-calls")
	fixture.gitStandIn = filepath.Join(stand, "the integrate job's git", "git")
	if makeError := os.MkdirAll(filepath.Dir(fixture.gitStandIn), 0o755); makeError != nil {
		test.Fatal(makeError)
	}
	if writeError := executable_testing.WriteFile(fixture.gitStandIn, []byte("#!/bin/sh\necho \"$*\" >> "+
		shellQuoted(fixture.gitCalls)+"\nexec "+shellQuoted(testGit)+" \"$@\"\n"), 0o755); writeError != nil {
		test.Fatal(writeError)
	}
	fixture.environment = append(fixture.environment, "HOME="+fixture.home, integrateJobShellVariable+"="+shell,
		"INTEGRATE_JOB_REPLIES="+fixture.replies, "INTEGRATE_JOB_PLATFORM="+runtime.GOOS+"/"+runtime.GOARCH,
		gateCohereVariable+"="+cohere, integrateJobMirrorVariable+"="+fixture.mirror,
		integrateJobGitVariable+"="+fixture.gitStandIn)
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
func (fixture *integrateJobFixture) merged() string {
	fixture.test.Helper()
	merge := fixture.unmirroredMerge()
	fixture.mirrorMerge(merge)
	return merge
}

// unmirroredMerge makes the merge Loom makes, of worktree a's head onto main, and puts only main in the mirror, as a
// job that starts before Loom's bridge has carried its merge finds it.
func (fixture *integrateJobFixture) unmirroredMerge() string {
	fixture.test.Helper()
	candidate := fixture.git(fixture.worktree("a"), "rev-parse", "HEAD")
	tree := strings.Fields(fixture.git(fixture.repository, "merge-tree", "--write-tree", "main", candidate))[0]
	merge := fixture.git(fixture.repository, "commit-tree", tree, "-p", "main", "-p", candidate, "-m",
		"Merge a onto main")
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, "main:refs/heads/main")
	return merge
}

// mirrorMerge puts merge in the mirror, as Loom's bridge carries it there.
func (fixture *integrateJobFixture) mirrorMerge(merge string) {
	fixture.test.Helper()
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, merge+":refs/heads/merge")
}

// reply writes what the stand-in bash answers from file in the replies directory.
func (fixture *integrateJobFixture) reply(file string, contents string) {
	fixture.test.Helper()
	writeFixtureFile(fixture.test, filepath.Join(fixture.replies, file), contents)
}

// integrateJobRan is what an integrate job did: its exit, what it printed to its output and its errors, and the tree it
// named on its last line, empty when it named none.
type integrateJobRan struct {
	exit       int
	output     string
	errors     string
	integrated string
	lastLine   string
}

// integrateJob runs integrate-job on merge in the job's own tree, which has no history, as Loom runs it, at main unless
// environment names another commit.
func (fixture *integrateJobFixture) integrateJob(merge string, environment ...string) integrateJobRan {
	fixture.test.Helper()
	return fixture.runJob([]string{"--merge", merge, "--task", fixtureTask, "--message", "a module measured and gated"},
		environment...)
}

// fill runs integrate-job --fill in the job's own tree, as Loom's cold fill runs it, at main unless environment names
// another commit.
func (fixture *integrateJobFixture) fill(environment ...string) integrateJobRan {
	fixture.test.Helper()
	return fixture.runJob([]string{"--fill", "--task", fixtureTask}, environment...)
}

// landedMain makes the merge Loom makes, of worktree a's head onto main, moves main to it, as a landing does, and puts
// that main in the mirror, as the bridge carries it, so a fill runs at a main whose tip is a landing. It returns it.
func (fixture *integrateJobFixture) landedMain() string {
	fixture.test.Helper()
	merge := fixture.unmirroredMerge()
	fixture.git(fixture.repository, "update-ref", "refs/heads/main", merge)
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, "main:refs/heads/main")
	return merge
}

// runJob runs integrate-job with arguments in the job's own tree, which has no history, at main unless environment
// names another commit.
func (fixture *integrateJobFixture) runJob(arguments []string, environment ...string) integrateJobRan {
	fixture.test.Helper()
	job := exec.Command(fixture.wrapper, append([]string{"integrate-job"}, arguments...)...)
	job.Dir = fixture.job
	job.Env = append(append(fixture.environment, integrateJobCommitVariable+"="+fixture.git(fixture.repository,
		"rev-parse", "main")), environment...)
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	job.Stdout, job.Stderr = &output, &errorOutput
	ran := integrateJobRan{}
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
	if integrated, found := strings.CutPrefix(ran.lastLine, "integrated "); found {
		ran.integrated = integrated
	}
	return ran
}

// arguments is what the stand-in bash was handed for kind, one a line, nil when that script never ran.
func (fixture *integrateJobFixture) arguments(kind string) []string {
	fixture.test.Helper()
	said := strings.TrimSuffix(string(readMaybeMissing(fixture.test, filepath.Join(fixture.replies,
		kind+".arguments"))), "\n")
	if said == "" {
		return nil
	}
	return strings.Split(said, "\n")
}

// landingSaid is what an integrate job's last lines say, as Loom's finisher reads them: the performance file, nil when
// unchanged, the tree, and the message.
type landingSaid struct {
	performance []byte
	unchanged   bool
	tree        string
	message     string
}

// readLanding reads an integrate job's output as Loom's finisher does, failing the test when its lines aren't in order.
func readLanding(test *testing.T, output string) landingSaid {
	test.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	begin := slices.Index(lines, integrateJobMarker+"message begin")
	end := slices.Index(lines, integrateJobMarker+"message end")
	if begin != 2 || end <= begin || end != len(lines)-2 {
		test.Fatalf("the landing's lines are out of order:\n%s", output)
	}
	said := landingSaid{message: strings.Join(lines[begin+1:end], "\n") + "\n"}
	performance, found := strings.CutPrefix(lines[0], integrateJobMarker+"performance ")
	tree, treeFound := strings.CutPrefix(lines[1], integrateJobMarker+"tree ")
	if !found || !treeFound || lines[len(lines)-1] != "integrated "+tree {
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

// rebuilt is the tree Loom's finisher makes from what the integrate job said: the merge's tree, with the performance
// file written when it changed.
func (fixture *integrateJobFixture) rebuilt(merge string, said landingSaid) string {
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

// A merge whose candidate is clean would integrate (#etpnnw1, #hnbb5vr): measured first, on a script handed no binary
// of main's since main holds no performance file, then tested, then checked, all on the merge's tree laid from the
// mirror. Nothing is committed or pushed: the last lines are the performance file, the tested tree, which the finisher
// rebuilds from the merge and that file to the same hash, and the landing's message, `Land #<task>: <what>` with the
// gate's account, the measurement's note and every step's seconds, then `integrated <tree>`. With
// COHERE_INTEGRATE_CACHE set the tests' script keeps the integrate job's cache, and unset it keeps the gate box's none;
// either way it reads the integrate job's own mirrors. Every git the job starts is the one it says it found (#5wj23dd):
// its own reads, the direct calls, and its packages', each through the stand-in, and both scripts carry that git's
// directory first on their path.
func TestAnIntegrateJobPassesAMergeAndSaysItsLanding(test *testing.T) {
	test.Parallel()
	for _, kept := range []bool{false, true} {
		test.Run(map[bool]string{false: "no kept cache", true: "a kept cache"}[kept], func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
			merge := fixture.merged()
			environment := []string{}
			cache := filepath.Join(test.TempDir(), "integrate cache")
			if kept {
				environment = append(environment, integrateJobCacheVariable+"="+cache)
			}
			ran := fixture.integrateJob(merge, environment...)
			if ran.exit != 0 || ran.integrated == "" {
				test.Fatalf("the integrate job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
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
			mirrors := filepath.Join(fixture.home, integrateJobScratch, "mirrors")
			wantCache := macBoxIntegrateCacheLine
			if kept {
				wantCache = "integrateCache='" + cache + "'"
			}
			// The gate box's run, but for its path, which puts the integrate job's git first (below).
			gate, gateError := withIntegrateJobGit(sharedBoxGate, sharedBoxPathLine, fixture.gitStandIn)
			if gateError != nil {
				test.Fatal(gateError)
			}
			if !strings.Contains(script, "\nmirrors='"+mirrors+"'; tries=1; pause=0\n"+wantCache+"\n") ||
				!strings.HasSuffix(script, gate) {
				test.Errorf(
					"the tests' script doesn't read the integrate job's mirrors with %s, or isn't the gate box's:"+
						"\n%.600s",
					wantCache,
					script,
				)
			}
			// The tests run as the integrate job's, kept cache or none, so the gate reads the module set Loom laid for
			// the job offline.
			if !strings.Contains(script, "\n"+integrateJobSetting+"\n") ||
				strings.Contains(script, "\n"+integrateJobLine+"\n") {
				test.Errorf("the tests' script doesn't say it is the integrate job's:\n%.600s", script)
			}
			// The tests skip what builds cohere-swift, which can't be built in an integrate job's sandbox, and cohere
			// on the tree checks every language and format but Swift.
			if unlaid := string(readMaybeMissing(test, filepath.Join(fixture.replies, "tests.swift"))); unlaid !=
				needs.SwiftUnlaidSetting {
				test.Errorf("the tests ran with %s=%q, want %q", needs.SwiftUnlaidVariable, unlaid,
					needs.SwiftUnlaidSetting)
			}
			checked := string(readMaybeMissing(test, fixture.cohereArguments))
			if checked != strings.Join(integrateJobCohereArguments, "\n")+"\n" || strings.Contains(checked, "swift") ||
				!strings.Contains(checked, "--languages\ngo,typescript\n") {
				test.Errorf("cohere on the tree was handed %q, want every language but swift:\n%s",
					integrateJobCohereArguments, checked)
			}
			// The measurement runs as the integrate job's, so it reads the module set Loom laid for the job.
			measuring := string(readMaybeMissing(test, filepath.Join(fixture.replies, "measure.script")))
			if !strings.Contains(measuring, "\n"+integrateJobSetting+"\n") || strings.Contains(measuring,
				"\n"+integrateJobLine+"\n") {
				test.Errorf("the measurement's script doesn't say it is the integrate job's:\n%.600s", measuring)
			}
			first := "export PATH=" + shellQuoted(filepath.Dir(fixture.gitStandIn)+":")
			for _, row := range []struct{ kind, script, line string }{
				{"tests", script, sharedBoxPathLine},
				{"measure", measuring, performancePathLine},
			} {
				if strings.Contains(row.script, "\n"+row.line+"\n") || !strings.Contains(row.script,
					"\n"+first+strings.TrimPrefix(row.line, "export PATH=")+"\n") {
					test.Errorf(
						"the %s script's path doesn't put the integrate job's git first:\n%s",
						row.kind,
						row.script,
					)
				}
			}
			if want := "cohere-dev: integrate-job runs the git at " + fixture.gitStandIn + "\n"; !strings.
				HasPrefix(ran.errors, want) {
				test.Errorf("the integrate job doesn't open with %q:\n%s", want, ran.errors)
			}
			said := readLanding(test, ran.output)
			// The integrate job's own read, its direct call for the tested tree's file, and its package's listing.
			called := string(readMaybeMissing(test, fixture.gitCalls))
			for _, want := range []string{"rev-list --parents -n 1 " + merge + "\n",
				"cat-file blob " + said.tree + ":" + performanceFileName + "\n", "ls-files --stage -z\n"} {
				if !strings.Contains(called, want) {
					test.Errorf(
						"the integrate job's git wasn't asked %q, so another git ran it; it was asked:\n%s",
						want,
						called,
					)
				}
			}
			if said.unchanged || !bytes.HasPrefix(said.performance, []byte(performanceHeader)) {
				test.Errorf("the performance line isn't a measured file:\n%s", said.performance)
			}
			if rebuilt := fixture.rebuilt(merge, said); rebuilt != said.tree {
				test.Errorf(
					"the finisher rebuilds %s from the lines, and the integrate job said %s",
					rebuilt,
					said.tree,
				)
			}
			for _, want := range []string{
				"Land #" + fixtureTask + ": a module measured and gated\n\n",
				"Gated by cohere-dev integrate-job on Shackler in ",
				": go test of the module on Shackler (" + runtime.GOOS + "/" + runtime.GOARCH + ", 2 packages, " +
					"shared uid, ",
				", go vet, cohere on the tree, 0 findings and no gaps.",
				"Measured on Shackler for " + performanceFileName + ": the first measurement, so nothing to compare.",
				"Steps: preflight ",
				"0 findings and no gaps. " + needs.SwiftUnlaid + ".\n",
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
// built that very binary and stops before any run, and the integrate job says the performance file is unchanged, the
// tested tree the merge's own, and that it is the same binary as main's, so not measured.
func TestAnIntegrateJobKeepsMainsFileForTheSameBinary(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	previous := sameBinaryPerformance(test, previousPerformance(test, performancePreviousCommit, performanceBox,
		performanceSpreadOf(9850, 9600, 10000), performanceSpreadOf(39000, 38000, 40000)))
	writeFixtureFile(test, filepath.Join(fixture.repository, performanceFileName), string(previous))
	fixture.git(fixture.repository, "add", performanceFileName)
	fixture.git(fixture.repository, "commit", "-q", "-m", "main's measurement before")
	fixture.reply("measure.run", performanceMarker+"tested COMMIT on darwin/arm64\n"+performanceMarker+"binary "+
		performanceFixtureBinary+"\n"+performanceSameBinaryMarker+"\n")
	merge := fixture.merged()
	ran := fixture.integrateJob(merge)
	if ran.exit != 0 || ran.integrated == "" {
		test.Fatalf("the integrate job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	if measured := fixture.arguments("measure"); len(measured) != 7 || measured[6] != performanceFixtureBinary {
		test.Errorf("the measurement was handed %q, want main's binary last", measured)
	}
	said := readLanding(test, ran.output)
	if !said.unchanged || said.tree != fixture.git(fixture.repository, "rev-parse", merge+"^{tree}") {
		test.Errorf("the integrate job said the file changed, or a tree %s other than the merge's", said.tree)
	}
	if want := "the same binary as main's (sha256 " + performanceFixtureBinary[:12] + "), so not measured"; !strings.
		Contains(said.message, want) {
		test.Errorf("the landing's message doesn't say %q:\n%s", want, said.message)
	}
}

// A red test refuses: the integrate job names the test and its output, exits 1, and says no landing.
func TestAnIntegrateJobRefusesARedTestNamingIt(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	fixture.reply("tests.fails", "example.com/fixture/alpha")
	ran := fixture.integrateJob(fixture.merged())
	if ran.exit != 1 || ran.integrated != "" || strings.Contains(ran.output, integrateJobMarker) {
		test.Fatalf("the integrate job exited %d and said %q, want 1 and no landing:\n%s", ran.exit, ran.output,
			ran.errors)
	}
	for _, want := range []string{"Shackler's tests on", "failed, so nothing landed",
		"example.com/fixture/alpha TestPlantedFailure", "planted: it fails", "integrate-job step tests refused in ",
		"integrate-job refused after "} {
		if !strings.Contains(ran.errors, want) {
			test.Errorf("the integrate job doesn't say %q:\n%s", want, ran.errors)
		}
	}
}

// A merge that changes what only Swift's checks judge refuses in the preflight, before anything is laid, measured or
// tested (#5wj23dd): cohere-swift can't be built in an integrate job's sandbox, so nothing there would check the
// change, and the refusal says so as the skips and the account do (needs.SwiftUnlaid). The Swift package and the Swift
// files of command/cohere and release's dispatch and packaging are each refused, named.
func TestAnIntegrateJobRefusesAMergeChangingWhatOnlySwiftChecks(test *testing.T) {
	test.Parallel()
	for _, changed := range []string{"swift_language/Package.swift", "command/cohere/swift_binary.go",
		"internal/release/dispatch/swift.go", "internal/release/dispatch/swift_tree_state_test.go",
		"internal/release/packaging/swift_package.go"} {
		test.Run(changed, func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			worktree := fixture.worktree("a")
			writeFixtureFile(test, filepath.Join(worktree, filepath.FromSlash(changed)), "// a Swift change\n")
			fixture.git(worktree, "add", ".")
			fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a Swift change"))
			ran := fixture.integrateJob(fixture.merged())
			if ran.exit != 1 || ran.integrated != "" {
				test.Fatalf("the integrate job exited %d, its last line %q, want 1 and no landing:\n%s", ran.exit,
					ran.lastLine, ran.errors)
			}
			for _, want := range []string{needs.SwiftUnlaid + ", so the merge's change to " + changed,
				"integrate-job step preflight refused in "} {
				if !strings.Contains(ran.errors, want) {
					test.Errorf("the integrate job doesn't say %q:\n%s", want, ran.errors)
				}
			}
			if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
				test.Errorf("a merge changing %s ran %q", changed, calls)
			}
		})
	}
}

// A merge that changes a file beside the Swift ones, in a package whose Swift files the preflight refuses, isn't
// refused: the guard names Swift's files, never their packages whole.
func TestAnIntegrateJobPassesAMergeChangingWhatSwiftDoesNotJudge(test *testing.T) {
	test.Parallel()
	for _, changed := range []string{"command/cohere/discovery.go", "internal/release/dispatch/progress.go",
		"internal/release/packaging/checksums.go", "internal/release/dispatch/swiftly.go"} {
		test.Run(changed, func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			worktree := fixture.worktree("a")
			writeFixtureFile(test, filepath.Join(worktree, filepath.FromSlash(changed)), "// a change Go judges\n")
			fixture.git(worktree, "add", ".")
			fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a change Go judges"))
			// The fixture scripts no measurement, so the job stops there: what matters is that it got past the
			// preflight and laid the merge.
			ran := fixture.integrateJob(fixture.merged())
			if strings.Contains(ran.errors, "preflight refused") || strings.Contains(ran.errors, "can't be judged") ||
				!strings.Contains(ran.errors, "integrate-job step lay ") {
				test.Fatalf("a merge changing %s didn't pass the preflight to be laid:\n%s", changed, ran.errors)
			}
		})
	}
}

// A merge group's commit names its pull request and carries its title (#hq61wfg, Jobs' road as Loom ruled it), and the
// title becomes what landed: its `(#<task>)` dropped once it names the job's task, held to one line of letters, digits,
// spaces and pullRequestTitleAllowed, and cut at a word to fit the landing's subject. One that breaks a rule is refused
// naming the pull request's number and the rule, never the title's words, and a merge that isn't a group's is no title.
func TestAPullRequestsTitleIsWhatLandedHeldToTheSet(test *testing.T) {
	test.Parallel()
	merged := func(title string) string { return "Merge pull request #12 from system-inc/branch\n\n" + title + "\n" }
	long := strings.Repeat("words ", 20) + "end"
	for _, row := range []struct {
		name, message, want, refusal string
	}{
		{name: "a title naming the task", message: merged("Test audit: cohere's tests read only its own tree (#t1)"),
			want: "Test audit: cohere's tests read only its own tree"},
		{name: "letters beyond English", message: merged("Größe, naïve (1/2) a+b_c-d; e."),
			want: "Größe, naïve (1/2) a+b_c-d; e."},
		{name: "no merge group", message: "Merge a onto main\n"},
		{name: "a long title", message: merged(long + " (#t1)"), want: strings.TrimSpace(strings.Repeat("words ", 10))},
		{name: "another task", message: merged("a title (#t2)"),
			refusal: "pull request #12's title names a task that isn't #t1, the one this job judges"},
		{name: "a hash inside", message: merged("a title #t1 here"), refusal: "pull request #12's title holds a " +
			"character outside letters, digits, spaces and " + pullRequestTitleAllowed + " at byte 8, so it's " +
			"refused and its words aren't repeated"},
		{name: "a shell's expansion", message: merged("a $(title)"), refusal: "pull request #12's title holds a " +
			"character outside letters, digits, spaces and " + pullRequestTitleAllowed + " at byte 2, so it's " +
			"refused and its words aren't repeated"},
		{name: "a zero width space", message: merged("a\u200btitle"), refusal: "pull request #12's title holds a " +
			"character outside letters, digits, spaces and " + pullRequestTitleAllowed + " at byte 1, so it's " +
			"refused and its words aren't repeated"},
		{name: "a second line", message: merged("a title\nrm everything"),
			refusal: "pull request #12's merge carries 4 lines, not its subject, a blank line and its title, so its " +
				"title can't be read"},
		{name: "a task alone", message: merged("(#t1)"),
			refusal: "pull request #12's title says nothing beside its task"},
		{name: "a word too long", message: merged(strings.Repeat("w", 70)), refusal: "pull request #12's title " +
			"opens with a word too long for the landing's subject's 72 characters"},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			title, refusal := pullRequestTitle(row.message, "t1")
			if title != row.want || refusal != row.refusal {
				test.Errorf("read %q refusing %q, want %q refusing %q", title, refusal, row.want, row.refusal)
			}
			if subject := "Land #t1: " + title; len([]rune(subject)) > subjectLimit {
				test.Errorf("the subject %q is %d characters, over %d", subject, len([]rune(subject)), subjectLimit)
			}
		})
	}
}

// A merge group's commit judged with no --message, as the PR road asks it, of a branch that brings several commits,
// lands with the pull request's title as what landed, where the branch's commits alone could say nothing.
func TestAnIntegrateJobWithNoMessageTakesThePullRequestsTitle(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	worktree := fixture.worktree("a")
	writeFixtureFile(test, filepath.Join(worktree, "alpha", "second.go"), "package alpha\n")
	fixture.git(worktree, "add", ".")
	fixture.git(worktree, "commit", "-q", "-m", fixtureSubject("a second commit"))
	candidate := fixture.git(worktree, "rev-parse", "HEAD")
	tree := strings.Fields(fixture.git(fixture.repository, "merge-tree", "--write-tree", "main", candidate))[0]
	merge := fixture.git(fixture.repository, "commit-tree", tree, "-p", "main", "-p", candidate, "-m",
		"Merge pull request #7 from system-inc/branch", "-m", "the census train, read whole (#"+fixtureTask+")")
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, "main:refs/heads/main", merge+":refs/heads/merge")
	job := exec.Command(fixture.wrapper, "integrate-job", "--merge", merge, "--task", fixtureTask)
	job.Dir = fixture.job
	job.Env = append(fixture.environment, integrateJobCommitVariable+"="+fixture.git(fixture.repository, "rev-parse",
		"main"))
	var output bytes.Buffer
	var errorOutput bytes.Buffer
	job.Stdout, job.Stderr = &output, &errorOutput
	if runError := job.Run(); runError != nil {
		test.Fatalf("the integrate job failed: %v\n%s", runError, errorOutput.String())
	}
	subject := strings.SplitN(readLanding(test, output.String()).message, "\n", 2)[0]
	if want := "Land #" + fixtureTask + ": the census train, read whole"; subject != want {
		test.Errorf("the landing's subject is %q, want %q", subject, want)
	}
}

// A merge Loom's bridge never carries to the mirror refuses in the preflight once the job has waited its bound, saying
// the mirror held no merge, the road's gap and never a red of the merge (Loom's condition 2 on #hq61wfg), and nothing
// is laid, measured or tested.
func TestAnIntegrateJobRefusesAMergeThatNeverReachesTheMirror(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	merge := fixture.unmirroredMerge()
	ran := fixture.integrateJob(merge, integrateJobMergeWaitVariable+"=1s")
	if ran.exit != 1 || ran.integrated != "" {
		test.Fatalf("the integrate job exited %d, its last line %q, want 1 and no landing:\n%s", ran.exit, ran.lastLine,
			ran.errors)
	}
	for _, want := range []string{"integrate-job waits up to 1s for the merge " + shortCommit(merge),
		fixture.mirror + " held no merge " + merge + " after 1s of waiting", "never a red of this merge",
		"integrate-job step preflight refused in "} {
		if !strings.Contains(ran.errors, want) {
			test.Errorf("the integrate job doesn't say %q:\n%s", want, ran.errors)
		}
	}
	if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
		test.Errorf("a merge the mirror never held ran %q", calls)
	}
}

// A merge the bridge carries to the mirror while the job waits for it is judged as one that was there from the start:
// the job says it waits, the merge arrives, and the preflight passes it on to be laid. The merge is carried only once
// the job has said it waits, so the wait is what let it through.
func TestAnIntegrateJobWaitsForItsMergeToReachTheMirror(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	merge := fixture.unmirroredMerge()
	job := exec.Command(fixture.wrapper, "integrate-job", "--merge", merge, "--task", fixtureTask, "--message",
		"a module measured and gated")
	job.Dir = fixture.job
	job.Env = append(append(fixture.environment, integrateJobCommitVariable+"="+fixture.git(fixture.repository,
		"rev-parse", "main")), integrateJobMergeWaitVariable+"=10m")
	errorOutput, pipeError := job.StderrPipe()
	if pipeError != nil {
		test.Fatal(pipeError)
	}
	if startError := job.Start(); startError != nil {
		test.Fatal(startError)
	}
	var said strings.Builder
	lines := bufio.NewScanner(errorOutput)
	carried := false
	for lines.Scan() {
		said.WriteString(lines.Text() + "\n")
		if !carried && strings.Contains(lines.Text(), "integrate-job waits up to 10m0s for the merge") {
			fixture.mirrorMerge(merge)
			carried = true
		}
	}
	if scanError := lines.Err(); scanError != nil {
		test.Fatal(scanError)
	}
	// Ignored: the fixture scripts no measurement, so the job ends there, and what it said is what's read.
	_ = job.Wait()
	if !carried {
		test.Fatalf("the integrate job never said it waits for its merge:\n%s", said.String())
	}
	// The fixture scripts no measurement, so the job stops there: what matters is that it got past the preflight and
	// laid the merge.
	if strings.Contains(said.String(), "held no merge") || !strings.Contains(said.String(), "integrate-job step lay ") {
		test.Errorf("a merge carried while the job waited wasn't laid:\n%s", said.String())
	}
}

// A merge whose first parent isn't the commit the job runs at refuses before anything is laid, measured or tested:
// its code wouldn't be the main it judges.
func TestAnIntegrateJobRefusesAMergeOntoAnotherMain(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	merge := fixture.merged()
	elsewhere := fixture.git(fixture.worktree("b"), "rev-parse", "HEAD")
	ran := fixture.integrateJob(merge, integrateJobCommitVariable+"="+elsewhere)
	if ran.exit != 1 || ran.integrated != "" {
		test.Fatalf("the integrate job exited %d, its last line %q, want 1 and no landing:\n%s", ran.exit, ran.lastLine,
			ran.errors)
	}
	if want := "and this job runs at " + shortCommit(elsewhere); !strings.Contains(ran.errors, want) {
		test.Errorf("the integrate job doesn't say %q:\n%s", want, ran.errors)
	}
	if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
		test.Errorf("a merge onto another main ran %q", calls)
	}
}

// The gate's Go cache by integrateCache (#s50bcnr), sharedBoxGate run for real with bash against a planted toolchain:
// unset, every go test carries -count=1 on a cache made empty for the job and gone after it, as on every gate box; set,
// no go test carries it, the cache is integrateCache's gocache, and it stays for the next job. No go test in the gate's
// scripts spells -count=1 itself, so the one setting is what every go test reads. integrateJob says the run is the
// integrate job's, kept cache or none, so set, go mod download and go mod verify read the module set `GOMODCACHE` names
// with `GOPROXY=off` and `GOSUMDB=off`, and unset, they read Go's default cache, the `GOMODCACHE` named never read.
// Loom's runner sets no COHERE_INTEGRATE_CACHE, and an integrate job keyed on the kept cache alone went to a proxy it
// can't reach. The runner's declared Go cache, its layer as `GOCACHE` and its `GOCACHEPROG` (#e94a6r4), is what an
// integrate job with no kept cache tests on, with no -count=1 and its layer left in place, and anywhere else the two
// are never read: a gate box's job runs on an empty cache of its own with no cache program.
func TestTheIntegrateJobsCacheIsKeptAndDropsCountOne(test *testing.T) {
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
	for _, row := range []struct {
		name                       string
		kept, integrateJob, runner bool
	}{
		{name: "unset"},
		{name: "set", kept: true, integrateJob: true},
		{name: "an integrate job with no kept cache", integrateJob: true},
		{name: "an integrate job on the runner's declared cache", integrateJob: true, runner: true},
		{name: "a gate box whose account names a cache program", runner: true},
	} {
		kept := row.kept
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			tree := test.TempDir()
			writeFixtureFile(test, filepath.Join(tree, "go.mod"), "module example.com/module\n\ngo 1.27\n")
			log := filepath.Join(test.TempDir(), "asked")
			temporary := test.TempDir()
			integrateCache := ""
			if kept {
				integrateCache = filepath.Join(test.TempDir(), "integrate cache")
			}
			integrateJob := ""
			if row.integrateJob {
				integrateJob = "1"
			}
			script := "set -euo pipefail\n" + `goroot="$1"; digest="$2"; node="$3"; nodeDigest="$4"; races="$5"; ` +
				`integrateCache="$6"; integrateJob="$7"; shift 7` + "\n" + sharedBoxGate
			gate := exec.Command("bash", "-c", script, "gate", goroot, digest, filepath.Join(tree, "no-node"),
				strings.Repeat("0", 64), "", integrateCache, integrateJob, "./...")
			laid := filepath.Join(test.TempDir(), "laid modules")
			layer := test.TempDir()
			gate.Dir = tree
			gate.Env = append(os.Environ(), "GATE_LOG="+log, "TMPDIR="+temporary, "GOCACHE="+layer,
				"GOMODCACHE="+laid, "GOPROXY=https://proxy.example.com")
			if row.runner {
				gate.Env = append(gate.Env, "GOCACHEPROG=loom-job-runner gocacheprog shared layer")
			}
			output, gateError := gate.CombinedOutput()
			if gateError != nil {
				test.Fatalf("the gate failed: %v\n%s", gateError, output)
			}
			asked := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, log))), "\n")
			if len(asked) != 3 || !strings.HasPrefix(asked[2], "test test ") {
				test.Fatalf("the gate asked for %q, want go mod download, go mod verify and the module's tests", asked)
			}
			modules, said := " | modcache  | proxy  | sum database ", boxModulesMarker+
				"Go's default cache, from the proxy\n"
			if row.integrateJob {
				modules, said = " | modcache "+
					laid+
					" | proxy off | sum database off", boxModulesMarker+"the box's laid set at "+laid+
					", offline\n"
			}
			if asked[0] != "download"+modules || asked[1] != "verify"+modules ||
				!strings.Contains(string(output), said) {
				test.Errorf("the gate asked go mod for %q and %q, want each with %q, saying %q:\n%s", asked[0],
					asked[1], modules, said, output)
			}
			if row.runner && row.integrateJob {
				if strings.Contains(asked[2], "-count") || !strings.Contains(asked[2], "| cache "+layer+" empty yes "+
					"| cache program loom-job-runner gocacheprog shared layer |") {
					test.Errorf("the tests ran %q, want no -count on the runner's layer %s and its program", asked[2],
						layer)
				}
				if information, statError := os.Stat(layer); statError != nil || !information.IsDir() {
					test.Errorf("the gate removed the runner's layer %s: %v", layer, statError)
				}
				return
			}
			if strings.Contains(asked[2], "| cache program loom-job-runner") {
				test.Errorf("outside an integrate job the tests ran %q, with the account's cache program", asked[2])
			}
			cache := filepath.Join(integrateCache, "gocache")
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
				test.Errorf("set, the tests ran %q, want no -count on the integrate job's cache %s", asked[2], cache)
			}
			if information, statError := os.Stat(cache); statError != nil || !information.IsDir() {
				test.Errorf("the integrate job's cache didn't stay after the gate: %v", statError)
			}
		})
	}
}

// The tests' temporary directory on a gate Mac, macBoxTemporaryBlock run for real with bash: a new one each run under
// the account's `TMPDIR`, while with integrateCache set it is one path each run inside the kept cache, emptied first,
// so go test's cache, which keys a test on the `TMPDIR` it read, replays across integrate jobs. Neither is ever under
// /tmp, which an integrate job in Shackler's sandbox may not write (J-7dhfcty).
func TestTheIntegrateJobsTemporaryDirectoryIsOnePath(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
	if strings.Contains(macBoxTemporaryBlock, "/tmp/") {
		test.Errorf("the block names a fixed path under /tmp:\n%s", macBoxTemporaryBlock)
	}
	temporary, kept := test.TempDir(), filepath.Join(test.TempDir(), "integrate cache")
	run := func(integrateCache string) string {
		test.Helper()
		bash := exec.Command("bash", "-s")
		bash.Env = append(os.Environ(), "TMPDIR="+temporary)
		bash.Stdin = strings.NewReader("set -euo pipefail\nrefuse() { echo \"refused $*\"; exit 1; }\nintegrateCache=" +
			shellQuoted(integrateCache) +
			"\n" +
			macBoxTemporaryBlock +
			"touch \"$gatetemp/left\"\necho \"$gatetemp\"\n")
		output, bashError := bash.CombinedOutput()
		if bashError != nil {
			test.Fatalf("the block failed: %v\n%s", bashError, output)
		}
		return strings.TrimSpace(string(output))
	}
	first, second := run(kept), run(kept)
	if first != second || first != filepath.Join(kept, "temporary") {
		test.Errorf("with a kept cache the runs' temporary directories are %s and %s, want %s", first, second,
			filepath.Join(kept, "temporary"))
	}
	if entries := listMaybeMissing(test, second); len(entries) != 1 {
		test.Errorf("the second run found %d entries in its temporary directory, want only its own", len(entries))
	}
	if cold, again := run(""), run(""); cold == again || !strings.HasPrefix(cold, temporary+"/cohere-gate.") {
		test.Errorf("without a kept cache the runs' temporary directories are %s and %s, want a new one each under %s",
			cold, again, temporary)
	}
}

// The integrate job's mirrors, read by macBoxScript run for real with bash: a merge held only in the cohere mirror, and
// its TypeScript pin held only in the TypeScript mirror beside it, are fetched through the integrate job's borrowing
// mirrors and checked out in the scratch tree, nothing copied, before the script reaches sharedBoxGate, which here
// finds no toolchain.
func TestTheIntegrateJobsMirrorsHoldTheMergeAndItsPin(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the gate's script is a bash script")
	directory := test.TempDir()
	environment := append(append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com"), gitWithoutMaintenance...)
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
	mirrors := filepath.Join(directory, "home", integrateJobScratch, "mirrors")
	for range 2 {
		// Laid again, as the next job lays them, they are kept and borrow anew.
		if layError := layIntegrateJobMirrors(filepath.Join(house, "cohere.git", "objects"), mirrors,
			[]string{filepath.Join(house, "TypeScript.git", "objects")}); layError != nil {
			test.Fatal(layError)
		}
	}
	script, scriptError := withIntegrateJobMirrors(macBoxScript, mirrors)
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
		test.Errorf("the integrate job's cohere mirror copied objects:\n%s", objects)
	}
}

// The account the Square rows (#ged8ew1), parsed from a fixture run's log: every step's seconds and the job's total,
// the kept cache before and after with the entries it gained, read from the entry count and said so, the packages that
// ran and replayed `(cached)` for the plain run and the -race run, and the free space and integrate path size before
// and after, each disk line with its own seconds.
func TestAnIntegrateJobsAccountLinesParse(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	fixture.reply("tests.cached", "example.com/fixture/alpha")
	cache := filepath.Join(test.TempDir(), "cache")
	writeFixtureFile(test, filepath.Join(cache, "gocache", "kept-entry"), "entry\n")
	fixture.reply("tests.grows", filepath.Join(cache, "gocache", "new-entry"))
	ran := fixture.integrateJob(fixture.merged(), integrateJobCacheVariable+"="+cache)
	if ran.exit != 0 {
		test.Fatalf("the integrate job exited %d:\n%s", ran.exit, ran.errors)
	}
	said := map[string]string{}
	for _, line := range strings.Split(ran.errors, "\n") {
		if rest, found := strings.CutPrefix(line, integrateJobMarker); found {
			fields := strings.Fields(rest)
			if len(fields) >= 2 {
				said[fields[0]+" "+fields[1]] = rest
			}
		}
	}
	seconds := regexp.MustCompile(`^step [a-z-]+ [0-9]+\.[0-9]s$`)
	for _, step := range []string{"preflight", "lay", "measure", "tests", "checks", "vet", "rule-cpu", "isolation",
		"docs-data", "self-check", "total"} {
		if !seconds.MatchString(said["step "+step]) {
			test.Errorf("the account's %s step reads %q", step, said["step "+step])
		}
	}
	for key, want := range map[string]*regexp.Regexp{
		"build cache": regexp.MustCompile(`^build cache gained 1 entries: compiled against cached is read from the ` +
			`entry count`),
		"packages tests":  regexp.MustCompile(`^packages tests 1 ran, 1 replayed \(cached\)$`),
		"packages -race":  regexp.MustCompile(`^packages -race (none run|[0-9]+ ran, 0 replayed \(cached\))$`),
		"disk before":     regexp.MustCompile(`^disk before integrate path [0-9]+ KiB, .* \(du -sk, [0-9.]+s\)$`),
		"disk after":      regexp.MustCompile(`^disk after integrate path [0-9]+ KiB, .* \(du -sk, [0-9.]+s\)$`),
		"measure skipped": nil,
	} {
		if want == nil {
			if said[key] != "" {
				test.Errorf("a measured job says %q", said[key])
			}
			continue
		}
		if !want.MatchString(said[key]) {
			test.Errorf("the account's %q line reads %q, want %s", key, said[key], want)
		}
	}
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^` + integrateJobMarker +
			`build cache before [0-9]+ KiB, 1 entries \(du -sk and a count, `),
		regexp.MustCompile(`(?m)^` + integrateJobMarker +
			`build cache after [0-9]+ KiB, 2 entries \(du -sk and a count, `),
		regexp.MustCompile(`(?m)^` + integrateJobMarker + `disk before free [0-9]+ KiB \(df -k, [0-9.]+s\)$`),
		regexp.MustCompile(`(?m)^` + integrateJobMarker + `disk after free [0-9]+ KiB \(df -k, [0-9.]+s\)$`),
	} {
		if !want.MatchString(ran.errors) {
			test.Errorf("the account doesn't match %s:\n%s", want, ran.errors)
		}
	}
}

// accountSteps are the names of the steps an integrate job's account says, in order: each `step <name> ...` line's
// name, a box step's `box`.
func accountSteps(errors string) []string {
	steps := []string{}
	for _, line := range strings.Split(errors, "\n") {
		if rest, found := strings.CutPrefix(line, integrateJobMarker+"step "); found {
			steps = append(steps, strings.Fields(rest)[0])
		}
	}
	return steps
}

// A fill judges the commit it runs at, so it takes no merge, and no message, since it lands nothing (#fnrchck): either
// is refused at the option parse, in words, exiting 2 as the options' errors do, before anything is laid or run.
func TestAFillRefusesAMergeOrAMessage(test *testing.T) {
	test.Parallel()
	for _, row := range []struct{ name, value string }{{"merge", ""}, {"message", "a module filled"}} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			value := row.value
			if value == "" {
				value = fixture.merged()
			}
			ran := fixture.runJob([]string{"--fill", "--" + row.name, value, "--task", fixtureTask})
			want := "integrate-job --fill judges the commit it runs at and lands nothing, so it takes no --" + row.name
			if ran.exit != 2 || !strings.Contains(ran.errors, want) {
				test.Fatalf("a fill handed --%s exited %d, want 2 saying %q:\n%s", row.name, ran.exit, want, ran.errors)
			}
			if _, statError := os.Stat(filepath.Join(fixture.home, integrateJobScratch)); statError == nil {
				test.Errorf("a fill handed --%s made its scratch before it refused", row.name)
			}
			if calls := readMaybeMissing(test, filepath.Join(fixture.replies, "calls")); len(calls) > 0 {
				test.Errorf("a fill handed --%s ran %q", row.name, calls)
			}
		})
	}
}

// A green fill at main's tip, a landing, says the tree it laid, main's own, as its one line of output, `filled <tree>`
// (#fnrchck): none of the finisher's lines, and never `integrated` or `landed`. Its tests are handed main's commit.
func TestAFillPassesMainsTipAndSaysFilled(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	tip := fixture.landedMain()
	ran := fixture.fill()
	if ran.exit != 0 {
		test.Fatalf("the fill exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	tree := fixture.git(fixture.repository, "rev-parse", tip+"^{tree}")
	if ran.output != "filled "+tree+"\n" || ran.integrated != "" {
		test.Errorf("the fill printed %q, want only `filled %s`", ran.output, tree)
	}
	for _, refused := range []string{"integrated ", "landed", integrateJobMarker + "performance ",
		integrateJobMarker + "tree ", integrateJobMarker + "message "} {
		if strings.Contains(ran.output, refused) || strings.Contains(ran.errors, refused) {
			test.Errorf("the fill says %q:\n%s\n%s", refused, ran.output, ran.errors)
		}
	}
	if tested := fixture.arguments("tests"); len(tested) == 0 || tested[0] != tip {
		test.Errorf("the fill's tests were handed %q, want main's tip %s first", tested, tip)
	}
}

// A fill measures nothing (#fnrchck): performanceScript never runs, so the stand-in bash is called for the tests
// alone, and the account says once that the measurement was skipped, never a measure step's seconds.
func TestAFillRunsNoMeasurement(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.landedMain()
	ran := fixture.fill()
	if ran.exit != 0 {
		test.Fatalf("the fill exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	if calls := string(readMaybeMissing(test, filepath.Join(fixture.replies, "calls"))); calls != "tests\n" {
		test.Errorf("the fill's scripts ran %q, want the tests alone", calls)
	}
	want := integrateJobMarker + "measure skipped: a fill measures nothing\n"
	if strings.Count(ran.errors, want) != 1 || slices.Contains(accountSteps(ran.errors), "measure") {
		test.Errorf("the account doesn't say %q once, or says a measure step:\n%s", want, ran.errors)
	}
}

// A fill's account says the steps an integration's does at the same tree, in the same order, but the measurement
// (#fnrchck), so the two compare step by step: the lay, the tests and their box's steps, and each check.
func TestAFillSaysTheStepsAnIntegrationDoes(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	merge := fixture.merged()
	integrated := fixture.integrateJob(merge)
	if integrated.exit != 0 {
		test.Fatalf("the integration exited %d:\n%s", integrated.exit, integrated.errors)
	}
	fixture.git(fixture.repository, "update-ref", "refs/heads/main", merge)
	fixture.git(fixture.repository, "push", "-q", fixture.mirror, "main:refs/heads/main")
	filled := fixture.fill()
	if filled.exit != 0 {
		test.Fatalf("the fill exited %d:\n%s", filled.exit, filled.errors)
	}
	want := slices.DeleteFunc(accountSteps(integrated.errors), func(step string) bool { return step == "measure" })
	if got := accountSteps(filled.errors); !slices.Equal(got, want) || !slices.Contains(got, "self-check") {
		test.Errorf("the fill's steps are %q, want the integration's but the measurement, %q", got, want)
	}
}

// An integrate job named no pool home hangs its pool from the job's own home, never the account's home in the system's
// user database, which in Shackler's sandbox is /var/empty, where the job can make nothing (J-5xseq90). The fixture
// names none here, as Loom's runner names none.
func TestAnIntegrateJobsPoolHangsFromItsOwnHome(test *testing.T) {
	test.Parallel()
	fixture := newIntegrateJobFixture(test)
	fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
	merge := fixture.merged()
	named := fixture.environment
	fixture.environment = make([]string, 0, len(named))
	for _, variable := range named {
		if !strings.HasPrefix(variable, homeVariable+"=") {
			fixture.environment = append(fixture.environment, variable)
		}
	}
	ran := fixture.integrateJob(merge)
	if ran.exit != 0 || ran.integrated == "" {
		test.Fatalf("the integrate job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
	}
	pool := filepath.Join(userCacheDirectoryIn(fixture.home), "cohere", "test-slots")
	if information, statError := os.Stat(pool); statError != nil || !information.IsDir() {
		test.Errorf("the integrate job's pool isn't under its home at %s: %v", pool, statError)
	}
}

// An integrate job handed a laid module set in `GOMODCACHE` runs its own go commands offline (#5wj23dd): the test
// listing, go vet and every other go it starts run with `GOPROXY=off` and `GOSUMDB=off`, whatever the account set,
// since the job has no network to reach either. Handed none, they run as the account set them.
func TestAnIntegrateJobsGoReadsItsLaidModulesOffline(test *testing.T) {
	test.Parallel()
	for _, laid := range []bool{false, true} {
		test.Run(map[bool]string{false: "no laid set", true: "a laid set"}[laid], func(test *testing.T) {
			test.Parallel()
			fixture := newIntegrateJobFixture(test)
			fixture.reply("measure.run", shacklerSays([]int64{9900, 9700, 9800}))
			merge := fixture.merged()
			fixture.environment = append(withoutModuleCache(fixture.environment),
				"GOPROXY=https://proxy.example.com", "GOSUMDB=sum.golang.org")
			want := " | proxy https://proxy.example.com | sum database sum.golang.org"
			if laid {
				fixture.environment = append(fixture.environment, "GOMODCACHE="+test.TempDir())
				want = " | proxy off | sum database off"
			}
			ran := fixture.integrateJob(merge)
			if ran.exit != 0 || ran.integrated == "" {
				test.Fatalf("the integrate job exited %d, its last line %q:\n%s", ran.exit, ran.lastLine, ran.errors)
			}
			called := strings.Split(strings.TrimSpace(string(readMaybeMissing(test, fixture.goCalls))), "\n")
			for _, asked := range []string{"list ", "vet "} {
				if !slices.ContainsFunc(called, func(line string) bool { return strings.HasPrefix(line, asked) }) {
					test.Errorf("the integrate job never ran go %s, so nothing here says how it ran:\n%s", asked,
						strings.Join(called, "\n"))
				}
			}
			for _, line := range called {
				if !strings.HasSuffix(line, want) {
					test.Errorf("a go the integrate job ran isn't under%s: %s", want, line)
				}
			}
		})
	}
}

// The integrate job's git (#5wj23dd): on a Mac the first that is an executable file of the Command Line Tools' git and
// the one in the developer directory `xcode-select -p` names, each laid under a root of the test's own, never
// /usr/bin/git, the shim whose xcrun can't write its cache in a job's sandbox, even reached through a link; and none
// of them a refusal naming both. Anywhere else it is the git on the path, whatever the developer tools hold.
func TestTheIntegrateJobsGitIsTheDeveloperToolsOwn(test *testing.T) {
	test.Parallel()
	xcode := "/Applications/Xcode.app/Contents/Developer"
	xcodeGit := filepath.Join(xcode, "usr", "bin", "git")
	for _, row := range []struct {
		name string
		// laid is each path laid under the root as a git, executable but those in plain, and linked lays the Command
		// Line Tools' git as a link to the shim.
		laid   []string
		plain  []string
		linked bool
		// developer is what `xcode-select -p` names, and empty, that it names none.
		developer string
		want      string
		refusal   []string
	}{
		{name: "the Command Line Tools' git comes first", laid: []string{commandLineToolsGit, xcodeGit, shimGit},
			developer: xcode, want: commandLineToolsGit},
		{name: "Xcode's git without the Command Line Tools'", laid: []string{xcodeGit, shimGit}, developer: xcode,
			want: xcodeGit},
		{name: "a Command Line Tools' git that doesn't execute", laid: []string{commandLineToolsGit, xcodeGit},
			plain: []string{commandLineToolsGit}, developer: xcode, want: xcodeGit},
		{name: "a developer directory whose git is the shim", laid: []string{shimGit}, developer: "/",
			refusal: []string{commandLineToolsGit, shimGit}},
		{name: "a Command Line Tools' git linked to the shim", laid: []string{shimGit}, linked: true, developer: xcode,
			refusal: []string{commandLineToolsGit, xcodeGit}},
		{name: "neither, the shim beside them", laid: []string{shimGit}, developer: xcode,
			refusal: []string{commandLineToolsGit, xcodeGit}},
		{name: "no developer directory named", laid: []string{shimGit},
			refusal: []string{commandLineToolsGit, "`xcode-select -p` named no developer directory"}},
	} {
		test.Run(row.name, func(test *testing.T) {
			test.Parallel()
			root := test.TempDir()
			// Each is only looked at, never run.
			for _, laid := range row.laid {
				writeFixtureFile(test, filepath.Join(root, laid), "#!/bin/sh\n")
				if slices.Contains(row.plain, laid) {
					continue
				}
				if chmodError := os.Chmod(filepath.Join(root, laid), 0o755); chmodError != nil {
					test.Fatal(chmodError)
				}
			}
			if row.linked {
				link := filepath.Join(root, commandLineToolsGit)
				if makeError := os.MkdirAll(filepath.Dir(link), 0o755); makeError != nil {
					test.Fatal(makeError)
				}
				if linkError := os.Symlink(filepath.Join(root, shimGit), link); linkError != nil {
					test.Fatal(linkError)
				}
			}
			developerDirectory := func() (string, error) {
				if row.developer == "" {
					return "", errors.New("xcode-select: error: no developer tools were found")
				}
				return row.developer, nil
			}
			found, realGitError := realGit("darwin", root, developerDirectory, func(string) (string, error) {
				test.Error("a Mac's git was looked for on the path")
				return "", errors.New("not on a Mac's path")
			})
			if row.want != "" {
				if realGitError != nil || found != filepath.Join(root, row.want) {
					test.Errorf("the git found is %q (%v), want %s", found, realGitError, filepath.Join(root, row.want))
				}
				return
			}
			if realGitError == nil {
				test.Fatalf("the git found is %s, want a refusal", found)
			}
			for _, want := range row.refusal {
				// A path is named under the root, and words as they are.
				if strings.HasPrefix(want, "/") {
					want = filepath.Join(root, want)
				}
				if !strings.Contains(realGitError.Error(), want) {
					test.Errorf("the refusal doesn't name %q: %v", want, realGitError)
				}
			}
		})
	}
	test.Run("off a Mac", func(test *testing.T) {
		test.Parallel()
		root := test.TempDir()
		writeFixtureFile(test, filepath.Join(root, commandLineToolsGit), "#!/bin/sh\n")
		if chmodError := os.Chmod(filepath.Join(root, commandLineToolsGit), 0o755); chmodError != nil {
			test.Fatal(chmodError)
		}
		onThePath := filepath.Join(test.TempDir(), "git")
		found, realGitError := realGit("linux", root, func() (string, error) {
			test.Error("xcode-select was asked off a Mac")
			return "", errors.New("no xcode-select")
		}, func(name string) (string, error) { return filepath.Join(filepath.Dir(onThePath), name), nil })
		if realGitError != nil || found != onThePath {
			test.Errorf("off a Mac the git found is %q (%v), want the path's %s", found, realGitError, onThePath)
		}
	})
}

// Each script the integrate job runs puts its git's directory first on its path (withIntegrateJobGit), quoted so bash
// reads a directory of any name back whole, and otherwise the path the script sets itself, which every other run of it
// keeps.
func TestTheIntegrateJobsScriptsPutItsGitFirstOnTheirPath(test *testing.T) {
	test.Parallel()
	needs.Tool(test, "bash", "the integrate job's scripts are bash scripts")
	git := filepath.Join(test.TempDir(), "the job's $git `dir`", "git")
	pathOf := func(line string) string {
		test.Helper()
		bash := exec.Command("bash", "-s")
		bash.Stdin = strings.NewReader("goroot=/goroot; nodePath=/node/bin:; node=/node\n" + line +
			"\nprintf '%s' \"$PATH\"\n")
		output, bashError := bash.CombinedOutput()
		if bashError != nil {
			test.Fatalf("bash: %v\n%s", bashError, output)
		}
		return string(output)
	}
	for _, row := range []struct{ name, script, line string }{
		{"the tests'", macBoxScript, sharedBoxPathLine},
		{"the measurement's", performanceScript, performancePathLine},
	} {
		replaced, replacedError := withIntegrateJobGit(row.script, row.line, git)
		if replacedError != nil {
			test.Errorf("%s script: %v", row.name, replacedError)
			continue
		}
		line, lineError := withIntegrateJobGit("\n"+row.line+"\n", row.line, git)
		if lineError != nil {
			test.Fatal(lineError)
		}
		line = strings.TrimSpace(line)
		if strings.Contains(replaced, "\n"+row.line+"\n") || !strings.Contains(replaced, "\n"+line+"\n") {
			test.Errorf("%s script keeps its own path line, or lacks the integrate job's:\n%s", row.name, replaced)
		}
		if got, want := pathOf(line), filepath.Dir(git)+":"+pathOf(row.line); got != want {
			test.Errorf("%s script's path is %q, want %q", row.name, got, want)
		}
	}
}
