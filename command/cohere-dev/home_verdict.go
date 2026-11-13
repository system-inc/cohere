package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/needs"
	"github.com/system-inc/cohere/internal/streams"
	"golang.org/x/mod/modfile"
)

// The landing gate's tests run on Home, Loom's day box, and only what Home could not run runs on the Mac (#acdf5mc).
// The module's go test is the heaviest thing the org puts on Kirk's Mac, minutes of every core, and that load spoils
// every quiet measurement taken there. Loom ruled (thread-294b4282) that cohere's land gate gets Home's day gaps.
//
// Everything that touches main stays on the Mac: the land lock, the merge, go vet, the rule CPU budget, the docs data,
// cohere on the tree and the fast-forward. Only `go test` moves, on the exact commit being landed. Home is Linux, so
// what it cannot run runs on the Mac in the same landing, as @system_cohere ruled: every test Home reported as skipped,
// whatever the reason, and every test in a file Linux never compiles, which Home cannot report because it does not
// exist there. Nothing is a list kept by hand. A test the Mac skips too stays a skip, named on the land line.
//
// Home failing a test refuses the landing. Home unreachable, refusing, out of its window or saying it tested another
// tree is no verdict at all, and the whole test run happens on the Mac, as it always did, said on the land line.

// homeGateVariable is "off" to test a landing on the Mac alone, the escape hatch, "kind" to test it as a Loom job of
// the land-gate kind (land_gate_job.go), or names a command run in place of Home's whole run, the push and the gap, for
// a test. Unset or "on", the landing's tests run on Home: the default since main's first whole pass on her, both
// halves, came back green at e74fbf79 (Nov 12, #acdf5mc).
const homeGateVariable = "COHERE_DEV_HOME_GATE"

// homeMarker opens the line Home's run prints before its tests: the commit it checked out and the platform it tests
// on, which the Mac reads to know whose verdict it holds and which files that platform never compiles.
const homeMarker = "cohere-dev-home: tested "

// homeRaceMarker is the line Home's run prints before its -race run of the raceGated packages, which the Mac reads
// apart from the module's run before it (#bnabymg).
const homeRaceMarker = "cohere-dev-home: race"

// homeRaceVariable carries the raceGated packages, comma-separated, to a stand-in for Home, as Home's script gets them
// as its third argument.
const homeRaceVariable = "COHERE_DEV_HOME_RACE"

// homeBox is the box the gate's tests run on.
const homeBox = "Home"

// The witness's hands-off window on Home, in UTC, when no land gate may take her: from 09:00, as Loom's limit for this
// gate stands (Operations, thread-28ff151a), half an hour before the witness's own 09:30.
const (
	witnessWindowOpens  = 9 * time.Hour
	witnessWindowCloses = 15 * time.Hour
)

// inWitnessWindow reports whether now is inside the witness's hands-off window on Home.
func inWitnessWindow(now time.Time) bool {
	utc := now.UTC()
	sinceMidnight := time.Duration(utc.Hour())*time.Hour + time.Duration(utc.Minute())*time.Minute
	return sinceMidnight >= witnessWindowOpens && sinceMidnight < witnessWindowCloses
}

// homeVerdict is what Home's run said: the commit and platform it tested, each package's result, the tests it failed
// with their output, and the tests it skipped, each by import path and top-level test name.
type homeVerdict struct {
	commit, operatingSystem, architecture string
	packagesPassed                        int
	packagesFailed                        []string
	failedTests                           []string
	failedOutput                          map[string][]string
	skipped                               map[string][]string
	// race is what Home's -race run of the raceGated packages said, nil when none was asked for.
	race *homeVerdict
	// refused is why the box refused to run the tests at all (sharedBoxRefusedMarker), empty when it ran them.
	refused string
	// optIn is every test or subtest, by import path, whose skip said it is opt-in (needs.OptInPhrase): no gate's
	// test, so the Mac never runs it again, and the land line names it as opt-in.
	optIn map[string][]string
	// raceReports is each race the run reported, whole, in the order it printed them. test2json gives a report to
	// whichever test it was framing when the report was printed, often one that passed, so a report is read from the
	// stream itself: printing only the failed tests' output dropped every stack (#bnabymg, Nov 12).
	raceReports []string
	// job is the land-gate job whose box ran the tests, empty when Home or a stand-in for her did.
	job string
}

// box names who ran a verdict's tests, as the gate's lines say it: Home, or the land-gate job Loom placed on a box.
func (verdict homeVerdict) box() string {
	if verdict.job == "" {
		return homeBox
	}
	return "land-gate job " + verdict.job
}

// raceReportStart and raceReportEnd are the race detector's lines around each report it prints.
const (
	raceReportStart = "WARNING: DATA RACE"
	raceReportEnd   = "=================="
)

// raceReportsShown is how many race reports a refused landing prints whole; the rest are counted.
const raceReportsShown = 3

// testEvent is one line of `go test -json`.
type testEvent struct {
	Action  string
	Package string
	Test    string
	Output  string
}

// readTestJson reads `go test -json` from output, with Home's marker line before it, and anything else it printed
// ignored: a gap prints its own lines around the command's.
func readTestJson(output []byte) homeVerdict {
	verdict := homeVerdict{failedOutput: map[string][]string{}, skipped: map[string][]string{},
		optIn: map[string][]string{}}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// reading is whether the stream is inside a race report, from its first line to the rule that ends it.
	reading := false
	for scanner.Scan() {
		line := scanner.Text()
		if why, isRefusal := strings.CutPrefix(line, sharedBoxRefusedMarker); isRefusal {
			verdict.refused = strings.TrimSpace(why)
			continue
		}
		if rest, isMarker := strings.CutPrefix(line, homeMarker); isMarker {
			commit, platform, _ := strings.Cut(rest, " on ")
			verdict.commit = strings.TrimSpace(commit)
			verdict.operatingSystem, verdict.architecture, _ = strings.Cut(strings.TrimSpace(platform), "/")
			continue
		}
		var event testEvent
		if !strings.HasPrefix(line, "{") || json.Unmarshal([]byte(line), &event) != nil || event.Package == "" {
			continue
		}
		key := event.Package + " " + event.Test
		switch {
		case event.Action == "output":
			verdict.failedOutput[key] = append(verdict.failedOutput[key], event.Output)
			text := strings.TrimSpace(event.Output)
			switch {
			case text == raceReportStart:
				reading = true
				verdict.raceReports = append(verdict.raceReports, event.Output)
			case reading && text == raceReportEnd:
				reading = false
			case reading:
				verdict.raceReports[len(verdict.raceReports)-1] += event.Output
			}
		case event.Test == "" && event.Action == "pass":
			verdict.packagesPassed++
		case event.Test == "" && event.Action == "fail":
			verdict.packagesFailed = append(verdict.packagesFailed, event.Package)
		case event.Action == "fail":
			verdict.failedTests = append(verdict.failedTests, key)
		// A package-level skip is a package with no test files, which has nothing to run again on the Mac.
		// An opt-in skip, said in the test's own output before its skip, is no gate's test, so it sends nothing to the
		// Mac: a top-level test with only opt-in skips under it, and its other subtests passed, is done.
		case event.Action == "skip" && event.Test != "" &&
			slices.ContainsFunc(verdict.failedOutput[key], func(line string) bool {
				return strings.Contains(line, needs.OptInPhrase)
			}):
			verdict.optIn[event.Package] = append(verdict.optIn[event.Package], event.Test)
		case event.Action == "skip" && event.Test != "":
			// A subtest skipped is its top-level test run again whole, since -run names top-level tests.
			name, _, _ := strings.Cut(event.Test, "/")
			if !slices.Contains(verdict.skipped[event.Package], name) {
				verdict.skipped[event.Package] = append(verdict.skipped[event.Package], name)
			}
		}
	}
	return verdict
}

// failed reports whether the verdict holds a failure: a failed package, which a build failure is too, or a test.
func (verdict homeVerdict) failed() bool {
	return len(verdict.packagesFailed) > 0 || len(verdict.failedTests) > 0
}

// testsNeverCompiledOn is every top-level test, by import path, in a file of modules that the lander's platform
// (landerSystem/landerArchitecture, the Mac's) compiles and the platform operatingSystem/architecture never does,
// by its name or its build constraint: Home cannot report a test that does not exist there, so the Mac runs it.
// The lander's platform is named rather than read from the build context, so a test of this runs the same on Home
// as on the Mac (#acdf5mc).
func testsNeverCompiledOn(
	root string,
	modules []project.Module,
	landerSystem string,
	landerArchitecture string,
	operatingSystem string,
	architecture string,
) (map[string][]string, error) {
	mac := build.Default
	mac.GOOS, mac.GOARCH = landerSystem, landerArchitecture
	other := build.Default
	other.GOOS, other.GOARCH = operatingSystem, architecture
	found := map[string][]string{}
	for _, module := range modules {
		moduleRoot := filepath.Join(root, module.Directory)
		walkError := filepath.WalkDir(moduleRoot, func(file string, entry fs.DirEntry, pathError error) error {
			if pathError != nil {
				return pathError
			}
			name := entry.Name()
			if entry.IsDir() {
				if file != moduleRoot && (name == "testdata" || strings.HasPrefix(name, ".") ||
					strings.HasPrefix(name, "_") || fileExists(filepath.Join(file, "go.mod"))) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(name, "_test.go") {
				return nil
			}
			directory := filepath.Dir(file)
			onMac, macError := mac.MatchFile(directory, name)
			onOther, otherError := other.MatchFile(directory, name)
			if macError != nil || otherError != nil || !onMac || onOther {
				return nil
			}
			parsed, parseError := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
			if parseError != nil {
				return parseError
			}
			relative, relativeError := filepath.Rel(moduleRoot, directory)
			if relativeError != nil {
				return relativeError
			}
			// The workspace leaves the root module's path empty, since its pattern is ./..., so it is read from the
			// module's go.mod: an empty one named the root's packages by their directory alone, which go test can't
			// load (command/cohere [setup failed], Nov 12).
			importPath := modulePath(moduleRoot, module.Path)
			if relative != "." {
				importPath = path.Join(importPath, filepath.ToSlash(relative))
			}
			for _, declaration := range parsed.Decls {
				function, isFunction := declaration.(*ast.FuncDecl)
				if isFunction && function.Recv == nil && strings.HasPrefix(function.Name.Name, "Test") &&
					!slices.Contains(found[importPath], function.Name.Name) {
					found[importPath] = append(found[importPath], function.Name.Name)
				}
			}
			return nil
		})
		if walkError != nil {
			return nil, fmt.Errorf("reading %s for tests %s/%s never compiles: %w", moduleRoot, operatingSystem,
				architecture, walkError)
		}
	}
	return found, nil
}

// modulePath is a module's import path: the one the workspace recorded, or, when it recorded none, the one its
// go.mod at moduleRoot declares.
func modulePath(moduleRoot string, recorded string) string {
	if recorded != "" {
		return recorded
	}
	// Ignored: a module with no readable go.mod has no path to read, and its tests are named by an empty one, which
	// go test refuses by name.
	goMod, _ := os.ReadFile(filepath.Join(moduleRoot, "go.mod"))
	return modfile.ModulePath(goMod)
}

func fileExists(file string) bool {
	_, statError := os.Stat(file)
	return statError == nil
}

// macRemainder is what the Mac runs after Home: Home's skips and the tests Home never compiled, merged, its packages
// sorted and each package's names sorted.
func macRemainder(skipped map[string][]string, neverCompiled map[string][]string) ([]string, []string) {
	names := map[string]bool{}
	packages := map[string]bool{}
	for _, each := range []map[string][]string{skipped, neverCompiled} {
		for importPath, tests := range each {
			packages[importPath] = true
			for _, test := range tests {
				names[test] = true
			}
		}
	}
	sortedPackages := make([]string, 0, len(packages))
	for importPath := range packages {
		sortedPackages = append(sortedPackages, importPath)
	}
	sort.Strings(sortedPackages)
	sortedNames := make([]string, 0, len(names))
	for name := range names {
		sortedNames = append(sortedNames, name)
	}
	sort.Strings(sortedNames)
	return sortedPackages, sortedNames
}

// optInNamed is every opt-in test of a box's run, as "Name (import path)", sorted, or empty when there were none.
func optInNamed(optIn map[string][]string) string {
	named := []string{}
	for importPath, tests := range optIn {
		for _, test := range tests {
			named = append(named, test+" ("+importPath+")")
		}
	}
	sort.Strings(named)
	return strings.Join(named, ", ")
}

// runHomeGate runs the module's tests on Home, or as a land-gate job for task, and returns what the box said, or why
// there is no verdict from it, which sends the whole test run to the Mac from Home and refuses the landing from a job.
// A verdict about another tree than commit is no verdict. setting is what homeGateVariable says.
func runHomeGate(
	worktree string,
	commit string,
	task string,
	patterns []string,
	races []string,
	now time.Time,
	setting string,
) (homeVerdict, string) {
	standIn := setting
	switch standIn {
	case "off":
		return homeVerdict{}, "Home is off for this landing (" + homeGateVariable + "=off)"
	case "", "on", landGateKindSetting:
		standIn = ""
	}
	var output []byte
	var runError error
	job := ""
	switch {
	case standIn != "":
		// A stand-in is no box, so Home's window is not its to keep.
		command := exec.Command(standIn, append([]string{commit}, patterns...)...)
		command.Dir = worktree
		command.Env = append(os.Environ(), homeRaceVariable+"="+strings.Join(races, ","))
		output, runError = command.CombinedOutput()
	// Loom places the job on a box of its choosing, never Home, so the witness's window is not its to keep.
	case setting == landGateKindSetting:
		var code int
		var unavailable string
		output, code, job, unavailable = runLandGateJob(worktree, commit, task, patterns, races)
		if unavailable != "" {
			return homeVerdict{}, unavailable
		}
		if code != 0 {
			runError = fmt.Errorf("exit status %d", code)
		}
	case inWitnessWindow(now):
		return homeVerdict{}, "it is the witness's window on Home, 09:00 to 15:00 UTC"
	default:
		output, runError = runOnHome(worktree, commit, patterns, races)
		if runError != nil && len(output) == 0 {
			return homeVerdict{}, "Home could not be reached: " + runError.Error()
		}
	}
	moduleOutput, raceOutput, raced := bytes.Cut(output, []byte("\n"+homeRaceMarker+"\n"))
	verdict := readTestJson(moduleOutput)
	verdict.job = job
	if raced {
		race := readTestJson(raceOutput)
		verdict.race = &race
	}
	raceFailed := verdict.race != nil && verdict.race.failed()
	switch {
	// A refusal is the box declining to vouch for this landing, read before the cases that would send the tests to
	// the Mac: a toolchain that isn't the pinned one, or a module cache that doesn't match go.sum, refuses the land.
	case verdict.refused != "":
		return verdict, ""
	case verdict.commit == "":
		return homeVerdict{}, fmt.Sprintf("%s's run never said what it tested (%s)", verdict.box(),
			lastWords(output, describeEnd(runError)))
	case verdict.commit != commit:
		return homeVerdict{}, fmt.Sprintf("%s tested %s, not %s", verdict.box(), shortCommit(verdict.commit),
			shortCommit(commit))
	case verdict.operatingSystem == "" || verdict.architecture == "":
		return homeVerdict{}, verdict.box() + "'s run never said which platform it tested on"
	case runError != nil && !verdict.failed() && !raceFailed:
		return homeVerdict{}, fmt.Sprintf("%s's run ended %s with no test failing", verdict.box(),
			describeEnd(runError))
	case verdict.packagesPassed == 0 && !verdict.failed():
		return homeVerdict{}, verdict.box() + "'s run passed no package"
	// A module run that passed is followed by the -race run, so one that passed with no -race run after it was cut
	// short, and says nothing of the races it was asked about.
	case len(races) > 0 && !verdict.failed() && verdict.race == nil:
		return homeVerdict{}, verdict.box() + "'s run said nothing of its -race run"
	case verdict.race != nil && verdict.race.packagesPassed == 0 && !raceFailed:
		return homeVerdict{}, verdict.box() + "'s -race run passed no package"
	}
	return verdict, ""
}

// The way to Home: the feed on Zach that Home fetches commits from, by sha, as `refs/builds/<sha>` (Computers,
// thread-28ff151a), and ahra's gap verb, which runs one command inside Home's quiet as a hand job on Zach.
const (
	homeFeed          = "loom-zach:/opt/loom-release-feed"
	homeGapBound      = "30m"
	homeHeldRetries   = 6
	homeHeldRetryWait = 45 * time.Second
	// gapRefusedExitCode is gap's exit when it refused the job, so it never ran (LoomGapCommandLineInterface.ts).
	gapRefusedExitCode = 3
)

// homeScript is what Home runs, from the land that runs it rather than from the tree it tests, so no branch can
// change what tests it. It fetches the commit and its TypeScript pin from the feed into a clone of its own that lasts
// across landings, checks them out in one tree at one path, so Go's test cache stays warm and keyed honestly, says
// which commit and platform it tests, and runs the module's tests as JSON, leaving out the events that only say a test
// started. Once they pass, it runs the raceGated packages under -race, after homeRaceMarker, its third argument
// naming them comma-separated (#bnabymg). The run itself is sharedBoxGate's, which trusts nothing Home kept (#adbkt5t).
const homeScript = `set -euo pipefail
commit="$1"; pin="$2"; races="$3"; shift 3
base=~/scratch/system_cohere_tools
mkdir -p "$base"; cd "$base"
[ -d cohere.git ] || git init -q --bare cohere.git
git -C cohere.git fetch -q zach-feed:/opt/loom-release-feed/cohere.git "$commit"
[ -e tree/.git ] || git -C cohere.git worktree add -q --detach "$base/tree" "$commit"
git -C tree checkout -q --detach --force "$commit"
git -C tree clean -q -f -d -x -e TypeScript
if [ -n "$pin" ]; then
  [ -d TypeScript.git ] || git init -q --bare TypeScript.git
  git -C TypeScript.git fetch -q zach-feed:/opt/loom-release-feed/TypeScript.git "$pin"
  if [ ! -e tree/TypeScript/.git ]; then
    rmdir tree/TypeScript 2>/dev/null || true
    git -C TypeScript.git worktree add -q --detach "$base/tree/TypeScript" "$pin"
  fi
  git -C tree/TypeScript checkout -q --detach --force "$pin"
fi
cd tree
goroot="` + sharedBoxGoroot + `"; digest="` + sharedBoxToolchainDigest + `"
` + sharedBoxGate

// runOnHome pushes commit and the TypeScript pin it records to the feed, then runs the module's tests on Home through
// gap, retrying a bounded while when another job holds her, and returns everything gap printed.
func runOnHome(worktree string, commit string, patterns []string, races []string) ([]byte, error) {
	if _, pushError := gitOutput(worktree, "push", "-q", homeFeed+"/cohere.git",
		commit+":refs/builds/"+commit); pushError != nil {
		return nil, fmt.Errorf("pushing %s to the feed: %w", shortCommit(commit), pushError)
	}
	// Ignored: a tree with no TypeScript gitlink needs no pin, and Home's script skips an empty one.
	pin, _ := gitOutput(worktree, "rev-parse", "--verify", "--quiet", commit+":TypeScript")
	if pin != "" {
		if _, pushError := gitOutput(filepath.Join(worktree, "TypeScript"), "push", "-q",
			homeFeed+"/TypeScript.git", pin+":refs/builds/"+pin); pushError != nil {
			return nil, fmt.Errorf("pushing the TypeScript pin %s to the feed: %w", shortCommit(pin), pushError)
		}
	}
	home, homeError := poolHome()
	if homeError != nil {
		return nil, homeError
	}
	ahraDirectory := filepath.Join(home, "Projects", "ahra")
	// Encoded, so nothing in the script is read by a shell before Home's bash: gap's command is one argument to ssh.
	remote := "echo " + base64.StdEncoding.EncodeToString([]byte(homeScript)) + " | base64 -d | bash -s -- " +
		commit + " '" + pin + "' '" + strings.Join(races, ",") + "' " + strings.Join(patterns, " ")
	for attempt := 1; ; attempt++ {
		gap := exec.Command(filepath.Join(ahraDirectory, "node_modules", ".bin", "ahra"), "loom", "gap",
			"--box", homeBox, "--within", homeGapBound, "--", "ssh", homeBox, remote)
		gap.Dir = ahraDirectory
		output, runError := gap.CombinedOutput()
		// Gap says "Home holds hand job <id> (<mind>, ...): one job per box" when another job has her, and "is held
		// by" for another hand's gap (LoomGapCommandLineInterface.ts); any other refusal is not worth asking again.
		if !gapHeld(runError, output) || attempt == homeHeldRetries {
			return output, runError
		}
		fmt.Fprintf(streams.Errors, "cohere-dev: %s is held by another job; asking again in %s (%d of %d)\n",
			homeBox, homeHeldRetryWait, attempt, homeHeldRetries)
		streams.Show(streams.Errors)
		time.Sleep(homeHeldRetryWait)
	}
}

// gapHeld reports whether gap refused a job because another holds Home: it exits gapRefusedExitCode and says "Home
// holds hand job <id> (<mind>, ...): one job per box" for a hand job, and "is held by" for another hand's gap
// (LoomGapCommandLineInterface.ts); any other refusal is not worth asking again. It reads the exit from the error
// itself, since exitCodeOf reports an error it can't read and a gap that ran has none: it printed "cohere-dev: <nil>"
// on every landing Home took (Nov 12).
func gapHeld(runError error, output []byte) bool {
	var exited *exec.ExitError
	return errors.As(runError, &exited) && exited.ExitCode() == gapRefusedExitCode &&
		(bytes.Contains(output, []byte("holds hand job")) || bytes.Contains(output, []byte("is held by")))
}

// terminalColor is a terminal's color sequence, which gap writes around its verdict words.
var terminalColor = regexp.MustCompile("\x1b\\[[0-9;]*m")

// lastWords is the last line of output that says something, its terminal colors taken out, so a fallback names why
// Home gave no verdict in gap's own words, "refused · ..." among them; fallback when it said nothing.
func lastWords(output []byte, fallback string) string {
	lines := strings.Split(strings.TrimSpace(terminalColor.ReplaceAllString(string(output), "")), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return last + ", " + fallback
	}
	return fallback
}

func describeEnd(runError error) string {
	if runError == nil {
		return "with exit 0"
	}
	return runError.Error()
}

// runModuleTests runs the module's tests for the gate: on Home, then on the Mac what Home could not run, or all of it
// on the Mac when Home gives no verdict, the -race run included, so no landing goes without one. As a land-gate job,
// no verdict refuses the landing instead. It returns the exit code and where they ran, as the land line says it after
// what was tested: "on Home (...)", "on a scratch box (...)" or "on the Mac, since ...".
func runModuleTests(
	worktree string,
	commit string,
	task string,
	covered project.Workspace,
	testing []string,
	now time.Time,
) (int, string) {
	patterns := covered.Patterns()
	races := racePatterns(worktree)
	setting := os.Getenv(homeGateVariable)
	verdict, unavailable := runHomeGate(worktree, commit, task, patterns, races, now, setting)
	// A job that gave no verdict refuses the landing rather than sending its tests to the Mac (#1v2shq7, item 2).
	if unavailable != "" && setting == landGateKindSetting {
		fmt.Fprintf(streams.Errors, "cohere-dev: no verdict from the land-gate job, so nothing landed, and its tests "+
			"never fall back to the Mac: %s\n", unavailable)
		return 1, ""
	}
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no tests on Home (%s), so they run here\n", unavailable)
		where := "on the Mac, since " + unavailable
		code := runTestsHere(worktree, testing, append([]string{"test"}, patterns...))
		if code != 0 || len(races) == 0 {
			return code, where
		}
		// Home's -race run is the Mac's when she gives none, after the module passes as hers is, through the gate's
		// token: a landing with no -race run at all let a race land whenever Home was busy (bef28c8e8, Nov 12).
		fmt.Fprintf(streams.Errors, "cohere-dev: the module passed, so its %d -race packages run here too\n",
			len(races))
		if raceCode := runTestsHere(worktree, testing, append([]string{"test", "-race"}, races...)); raceCode != 0 {
			fmt.Fprintf(streams.Errors, "cohere-dev: the -race run here failed, so nothing landed\n")
			return raceCode, ""
		}
		return 0, where + fmt.Sprintf(", then %d packages again under -race", len(races))
	}
	if verdict.refused != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s refused to test this landing, so nothing landed: %s\n",
			verdict.box(), verdict.refused)
		return 1, ""
	}
	platform := verdict.operatingSystem + "/" + verdict.architecture
	if verdict.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s failed, so nothing landed:\n", verdict.box(),
			platform)
		for _, failure := range slices.Concat(verdict.failedTests, verdict.packagesFailed) {
			fmt.Fprintf(streams.Errors, "  %s\n", failure)
			for _, line := range verdict.failedOutput[failure] {
				fmt.Fprintf(streams.Errors, "    %s", line)
			}
			for _, line := range verdict.failedOutput[failure+" "] {
				fmt.Fprintf(streams.Errors, "    %s", line)
			}
		}
		return 1, ""
	}
	if verdict.race != nil && verdict.race.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run on %s failed, so nothing landed:\n", verdict.box(),
			platform)
		for _, failure := range slices.Concat(verdict.race.failedTests, verdict.race.packagesFailed) {
			fmt.Fprintf(streams.Errors, "  %s\n", failure)
			output := verdict.race.failedOutput
			for _, line := range slices.Concat(output[failure], output[failure+" "]) {
				fmt.Fprintf(streams.Errors, "    %s", line)
			}
		}
		printRaceReports(verdict.race.raceReports)
		return 1, ""
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s passed, %d packages\n", verdict.box(), platform,
		verdict.packagesPassed)
	neverCompiled, neverCompiledError := testsNeverCompiledOn(worktree, covered.Modules, runtime.GOOS, runtime.GOARCH,
		verdict.operatingSystem, verdict.architecture)
	if neverCompiledError != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", neverCompiledError)
		return 2, ""
	}
	packages, names := macRemainder(verdict.skipped, neverCompiled)
	where := fmt.Sprintf("on Home (%s, %d packages, %s)", platform, verdict.packagesPassed, sharedBoxTrust)
	if verdict.job != "" {
		where = fmt.Sprintf("on a scratch box (%s, %d packages, %s; land-gate job %s)", platform,
			verdict.packagesPassed, sharedBoxTrust, verdict.job)
	}
	if verdict.race != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run passed, %d packages\n", verdict.box(),
			verdict.race.packagesPassed)
		where += fmt.Sprintf(", %d of them again under -race", verdict.race.packagesPassed)
	}
	optIn := optInNamed(verdict.optIn)
	if optIn != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: opt-in, so run by no gate: %s\n", optIn)
	}
	if len(names) == 0 {
		if optIn != "" {
			return 0, where + ", nothing it skipped but opt-in runs, which no gate runs: " + optIn
		}
		return 0, where + ", nothing it skipped"
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %d tests %s skipped or never compiled run here: %s\n", len(names),
		verdict.box(), strings.Join(names, ", "))
	// Read as Home's run is, so a test the Mac skips too is named, never counted as run.
	here := exec.Command("go", slices.Concat([]string{"test", "-json", "-run", "^(" + strings.Join(names, "|") + ")$"},
		packages)...)
	here.Dir, here.Env = worktree, testing
	output, runError := here.CombinedOutput()
	mac := readTestJson(output)
	if mac.failed() || (runError != nil && !mac.failed()) {
		fmt.Fprintf(streams.Errors, "cohere-dev: the tests %s skipped failed here, so nothing landed (%s):\n",
			verdict.box(), describeEnd(runError))
		for _, failure := range slices.Concat(mac.failedTests, mac.packagesFailed) {
			fmt.Fprintf(streams.Errors, "  %s\n", failure)
			for _, line := range slices.Concat(mac.failedOutput[failure], mac.failedOutput[failure+" "]) {
				fmt.Fprintf(streams.Errors, "    %s", line)
			}
		}
		return 1, ""
	}
	stillSkipped := []string{}
	for importPath, tests := range mac.skipped {
		for _, test := range tests {
			stillSkipped = append(stillSkipped, test+" ("+importPath+")")
		}
	}
	sort.Strings(stillSkipped)
	where += fmt.Sprintf(", then %d tests it skipped or never compiled on the Mac (%s)", len(names),
		strings.Join(names, ", "))
	if optIn != "" {
		where += "; opt-in, which no gate runs: " + optIn
	}
	if len(stillSkipped) > 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: skipped on the Mac too, so run nowhere: %s\n",
			strings.Join(stillSkipped, ", "))
		where += "; skipped on the Mac too: " + strings.Join(stillSkipped, ", ")
	}
	return 0, where
}

// printRaceReports prints the first raceReportsShown of a run's race reports whole, and counts the rest, so a refused
// landing shows where each race is without a lander pulling the stacks from Home by hand.
func printRaceReports(reports []string) {
	if len(reports) == 0 {
		return
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %d races reported, the first %d whole:\n", len(reports),
		min(len(reports), raceReportsShown))
	for _, report := range reports[:min(len(reports), raceReportsShown)] {
		for _, line := range strings.SplitAfter(strings.TrimRight(report, "\n"), "\n") {
			fmt.Fprintf(streams.Errors, "    %s", strings.TrimRight(line, "\n")+"\n")
		}
	}
	if len(reports) > raceReportsShown {
		fmt.Fprintf(streams.Errors, "cohere-dev: and %d more races, not shown\n", len(reports)-raceReportsShown)
	}
}

// runTestsHere runs a go test on the Mac, in the gate's environment, writing to the terminal, and returns its exit
// code.
func runTestsHere(worktree string, testing []string, arguments []string) int {
	test := exec.Command("go", arguments...)
	test.Dir, test.Env = worktree, testing
	test.Stdout, test.Stderr = os.Stdout, os.Stderr
	// The command writes to the terminal itself, so what this run said first is delivered first.
	streams.Show(streams.Output)
	streams.Show(streams.Errors)
	if runError := test.Run(); runError != nil {
		return exitCodeOf(runError)
	}
	return 0
}
