package main

import (
	"bufio"
	"bytes"
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

	"github.com/system-inc/cohere/internal/go_engine/project"
	"github.com/system-inc/cohere/internal/needs"
	"github.com/system-inc/cohere/internal/streams"
	"golang.org/x/mod/modfile"
)

// The landing gate's tests run on a house Mac reached by Loom's gap, never on Kirk's Mac (#rr818c8). The module's go
// test is the heaviest thing the org puts on Kirk's Mac, minutes of every core, and that load spoils every quiet
// measurement taken there. It first moved to Home (#acdf5mc), and when Loom retired her, every landing fell back to
// Kirk's Mac, so @system_cohere ruled that the gate's box is a setting of house Macs (mac_box_gate.go), and that no
// verdict from them refuses the landing rather than falling back.
//
// Everything that touches main stays on Kirk's Mac: the land lock, the merge, go vet, the rule CPU budget, the docs
// data, cohere on the tree and the fast-forward. Only `go test` moves, on the exact commit being landed, the -race run
// of the raceGated packages with it. A gate box is darwin, as Kirk's Mac is, so nothing it skipped runs again here: a
// test it skipped is named on the land line as run nowhere.
//
// A box failing a test refuses the landing. A box that refuses, gives no verdict, or says it tested another tree
// refuses it too, in its own words, and every box held for as long as the land waits refuses it, naming them.

// gateVariable is gateOffSetting to test a landing on Kirk's Mac alone, the escape, landGateKindSetting to test it as a
// Loom job of the land-gate kind (land_gate_job.go), or names a command run in place of the whole road, the push and
// the gap, for a test. Unset or "on", the landing's tests run on the house Macs gateBoxesVariable names.
const gateVariable = "COHERE_DEV_GATE"

// gateOffSetting is the gateVariable setting that tests a landing on Kirk's Mac alone, named on its land line.
const gateOffSetting = "off"

// boxMarker opens the line a gate box's run prints before its tests: the commit it checked out and the platform it
// tests on, which Kirk's Mac reads to know whose verdict it holds.
const boxMarker = "cohere-dev-gate: tested "

// boxRaceMarker is the line a gate box's run prints before its -race run of the raceGated packages, which Kirk's Mac
// reads apart from the module's run before it (#bnabymg).
const boxRaceMarker = "cohere-dev-gate: race"

// boxRaceVariable carries racePatterns' tokens, comma-separated, each `./<directory>` or `./<directory>@<run>`, to a
// stand-in for the road, as the box's script gets them as its third argument.
const boxRaceVariable = "COHERE_DEV_GATE_RACE"

// standInBox names the box of a verdict a stand-in for the road gave, which is no box at all.
const standInBox = "the stand-in"

// boxVerdict is what a gate box's run said: the commit and platform it tested, each package's result, the tests it
// failed with their output, and the tests it skipped, each by import path and test name.
type boxVerdict struct {
	commit, operatingSystem, architecture string
	packagesPassed                        int
	packagesFailed                        []string
	failedTests                           []string
	failedOutput                          map[string][]string
	skipped                               map[string][]string
	// race is what the box's -race run of the raceGated packages said, nil when none was asked for.
	race *boxVerdict
	// refused is why the box refused to run the tests at all (sharedBoxRefusedMarker), empty when it ran them.
	refused string
	// optIn is every test or subtest, by import path, whose skip said it is opt-in (needs.OptInPhrase): no gate's
	// test, so the land line names it as opt-in rather than as run nowhere.
	optIn map[string][]string
	// raceReports is each race the run reported, whole, in the order it printed them. test2json gives a report to
	// whichever test it was framing when the report was printed, often one that passed, so a report is read from the
	// stream itself: printing only the failed tests' output dropped every stack (#bnabymg, Nov 12).
	raceReports []string
	// timeouts is, for each package whose test binary hit go test's -timeout, everything it printed from its timeout's
	// panic on: the tests still running and every goroutine's stack. test2json gives that text to whichever test it
	// was framing, which never ends with a pass or a fail, so printing only the failed tests' output dropped it and a
	// timeout read as a bare `FAIL` with no test named (internal/go_engine on Sun2, Nov 23, #r28e4nh).
	timeouts map[string][]string
	// box names who ran the tests, as the gate's lines say it: the Mac the gap ran on, the land-gate job Loom placed,
	// or the stand-in. It is the road's to set, since the box's own output never names it.
	box string
	// job is the land-gate job whose box ran the tests, empty when a gate box or a stand-in did.
	job string
	// corpora names the corpora the box held, each at its commit, and unlaidCorpora says of each corpus she didn't
	// hold why it wasn't laid (#5r0vrte). Both are the road's to set; only a gate box holds corpora.
	corpora       string
	unlaidCorpora []string
}

// timeoutPanic opens what a test binary prints when go test's -timeout ends it (testing's own words).
const timeoutPanic = "panic: test timed out after "

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

// readTestJson reads `go test -json` from output, with the box's marker line before it, and anything else it printed
// ignored: a gap prints its own lines around the command's.
func readTestJson(output []byte) boxVerdict {
	verdict := boxVerdict{failedOutput: map[string][]string{}, skipped: map[string][]string{},
		optIn: map[string][]string{}, timeouts: map[string][]string{}}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// reading is whether the stream is inside a race report, from its first line to the rule that ends it.
	reading := false
	// timedOut is each package whose timeout panic the stream is inside, from its panic's line to the package's end.
	timedOut := map[string]bool{}
	for scanner.Scan() {
		line := scanner.Text()
		if why, isRefusal := strings.CutPrefix(line, sharedBoxRefusedMarker); isRefusal {
			verdict.refused = strings.TrimSpace(why)
			continue
		}
		if rest, isMarker := strings.CutPrefix(line, boxMarker); isMarker {
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
		// A timeout's text is kept once, as the package's, never again as the output of the test it was framed in.
		case event.Action == "output" && (timedOut[event.Package] ||
			strings.HasPrefix(strings.TrimSpace(event.Output), timeoutPanic)):
			timedOut[event.Package] = true
			verdict.timeouts[event.Package] = append(verdict.timeouts[event.Package], event.Output)
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
			timedOut[event.Package] = false
			verdict.packagesPassed++
		case event.Test == "" && event.Action == "fail":
			timedOut[event.Package] = false
			verdict.packagesFailed = append(verdict.packagesFailed, event.Package)
		case event.Action == "fail":
			verdict.failedTests = append(verdict.failedTests, key)
		// A package-level skip is a package with no test files, which skipped no test. An opt-in skip, said in the
		// test's own output before its skip, is no gate's test, so it is named as opt-in, never as run nowhere.
		case event.Action == "skip" && event.Test != "" &&
			slices.ContainsFunc(verdict.failedOutput[key], func(line string) bool {
				return strings.Contains(line, needs.OptInPhrase)
			}):
			verdict.optIn[event.Package] = append(verdict.optIn[event.Package], event.Test)
		// A subtest is named whole, since its parent and the subtests beside it ran.
		case event.Action == "skip" && event.Test != "":
			if !slices.Contains(verdict.skipped[event.Package], event.Test) {
				verdict.skipped[event.Package] = append(verdict.skipped[event.Package], event.Test)
			}
		}
	}
	return verdict
}

// failed reports whether the verdict holds a failure: a failed package, which a build failure is too, or a test.
func (verdict boxVerdict) failed() bool {
	return len(verdict.packagesFailed) > 0 || len(verdict.failedTests) > 0
}

// testsNeverCompiledOn is every top-level test, by import path, in a file of modules that the lander's platform
// (landerSystem/landerArchitecture, Kirk's Mac's) compiles and the platform operatingSystem/architecture never does,
// by its name or its build constraint: a box on another platform cannot report a test that does not exist there, so
// the land line names it as run nowhere. The lander's platform is named rather than read from the build context, so a
// test of this runs the same on any machine (#acdf5mc).
func testsNeverCompiledOn(
	root string,
	modules []project.Module,
	landerSystem string,
	landerArchitecture string,
	operatingSystem string,
	architecture string,
) (map[string][]string, error) {
	lander := build.Default
	lander.GOOS, lander.GOARCH = landerSystem, landerArchitecture
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
			onLander, landerError := lander.MatchFile(directory, name)
			onOther, otherError := other.MatchFile(directory, name)
			if landerError != nil || otherError != nil || !onLander || onOther {
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

// testsNamed is every test of a box's run, as "Name (import path)", sorted.
func testsNamed(tests map[string][]string) []string {
	named := []string{}
	for importPath, names := range tests {
		for _, name := range names {
			named = append(named, name+" ("+importPath+")")
		}
	}
	sort.Strings(named)
	return named
}

// runBoxGate runs the module's tests on a gate box, on the road setting names, and returns what the box said, or why
// there is no verdict from it, which refuses the landing. A verdict about another tree than commit is no verdict.
// setting is what gateVariable says, never gateOffSetting, which runModuleTests keeps on Kirk's Mac.
func runBoxGate(
	worktree string,
	commit string,
	task string,
	patterns []string,
	races []string,
	setting string,
) (boxVerdict, string) {
	var output []byte
	var runError error
	box := ""
	job := ""
	corporaLaid := ""
	unlaidCorpora := []string{}
	switch setting {
	case "", "on":
		boxes, boxesError := gateBoxes(os.Getenv(gateBoxesVariable))
		if boxesError != nil {
			return boxVerdict{}, boxesError.Error()
		}
		wait, waitError := waitSetting(gateWaitVariable, os.Getenv(gateWaitVariable), gateWaitDefault)
		if waitError != nil {
			return boxVerdict{}, waitError.Error()
		}
		corpora, unlaid, corporaError := gateCorpora(worktree, commit)
		if corporaError != nil {
			return boxVerdict{}, "the corpora this landing pins can't be read: " + corporaError.Error()
		}
		corporaLaid, unlaidCorpora = corporaNamed(corpora), unlaid
		var unavailable string
		output, box, unavailable, runError = runOnGateBoxes(worktree, commit, patterns, races, corpora, boxes, wait)
		if unavailable != "" {
			return boxVerdict{}, unavailable
		}
		if runError != nil && len(output) == 0 {
			return boxVerdict{}, "the gap on " + box + " could not run: " + runError.Error()
		}
	case landGateKindSetting:
		var code int
		var unavailable string
		output, code, job, unavailable = runLandGateJob(worktree, commit, task, patterns, races)
		if unavailable != "" {
			return boxVerdict{}, unavailable
		}
		box = "land-gate job " + job
		if code != 0 {
			runError = fmt.Errorf("exit status %d", code)
		}
	default:
		command := exec.Command(setting, append([]string{commit}, patterns...)...)
		command.Dir = worktree
		command.Env = append(os.Environ(), boxRaceVariable+"="+strings.Join(races, ","))
		output, runError = command.CombinedOutput()
		box = standInBox
	}
	moduleOutput, raceOutput, raced := bytes.Cut(output, []byte("\n"+boxRaceMarker+"\n"))
	verdict := readTestJson(moduleOutput)
	verdict.box, verdict.job = box, job
	verdict.corpora, verdict.unlaidCorpora = corporaLaid, unlaidCorpora
	if raced {
		race := readTestJson(raceOutput)
		verdict.race = &race
	}
	raceFailed := verdict.race != nil && verdict.race.failed()
	switch {
	// A refusal is the box declining to vouch for this landing: a mirror that doesn't hold the commit, a toolchain
	// that isn't the pinned one, or a module cache that doesn't match go.sum.
	case verdict.refused != "":
		return verdict, ""
	case verdict.commit == "":
		return boxVerdict{}, fmt.Sprintf("%s's run never said what it tested (%s)", box,
			lastWords(output, describeEnd(runError)))
	case verdict.commit != commit:
		return boxVerdict{}, fmt.Sprintf("%s tested %s, not %s", box, shortCommit(verdict.commit),
			shortCommit(commit))
	case verdict.operatingSystem == "" || verdict.architecture == "":
		return boxVerdict{}, box + "'s run never said which platform it tested on"
	case runError != nil && !verdict.failed() && !raceFailed:
		return boxVerdict{}, fmt.Sprintf("%s's run ended %s with no test failing (%s)", box,
			describeEnd(runError), lastWords(output, describeEnd(runError)))
	case verdict.packagesPassed == 0 && !verdict.failed():
		return boxVerdict{}, box + "'s run passed no package"
	// A module run that passed is followed by the -race run, so one that passed with no -race run after it was cut
	// short, and says nothing of the races it was asked about.
	case len(races) > 0 && !verdict.failed() && verdict.race == nil:
		return boxVerdict{}, box + "'s run said nothing of its -race run"
	case verdict.race != nil && verdict.race.packagesPassed == 0 && !raceFailed:
		return boxVerdict{}, box + "'s -race run passed no package"
	}
	return verdict, ""
}

// gapRefusedExitCode is gap's exit when it refused the command, so it never ran (LoomGapCommandLineInterface.ts).
const gapRefusedExitCode = 3

// gapHeld reports whether gap refused a command because another holds the box: it exits gapRefusedExitCode and says
// "<box> stayed held by <who> for <n> minutes" when a Mac's claim outlasted its wait (LoomHandGap.ts), "is held by"
// for another hand's gap, or "<box> holds hand job <id> (<mind>, ...): one job per box" for a hand job
// (LoomGapCommandLineInterface.ts); any other refusal is the box's word on this landing. It reads the exit from the
// error itself, since exitCodeOf reports an error it can't read and a gap that ran has none: it printed
// "cohere-dev: <nil>" on every landing Home took (Nov 12).
func gapHeld(runError error, output []byte) bool {
	var exited *exec.ExitError
	return errors.As(runError, &exited) && exited.ExitCode() == gapRefusedExitCode &&
		(bytes.Contains(output, []byte("held by")) || bytes.Contains(output, []byte("holds hand job")))
}

// terminalColor is a terminal's color sequence, which gap writes around its verdict words.
var terminalColor = regexp.MustCompile("\x1b\\[[0-9;]*m")

// lastWords is the last line of output that says something, its terminal colors taken out, so a refusal names why a
// box gave no verdict in gap's own words, "refused · ..." among them; fallback when it said nothing.
func lastWords(output []byte, fallback string) string {
	lines := strings.Split(strings.TrimSpace(terminalColor.ReplaceAllString(string(output), "")), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		return last + ", " + fallback
	}
	return fallback
}

// failureReport is each failed test and package with its output, and each package's timeout whole, every goroutine's
// stack included, so a timeout names what it was waiting on.
func (verdict boxVerdict) failureReport() string {
	report := strings.Builder{}
	for _, failure := range slices.Concat(verdict.failedTests, verdict.packagesFailed) {
		fmt.Fprintf(&report, "  %s\n", failure)
		for _, line := range slices.Concat(verdict.failedOutput[failure], verdict.failedOutput[failure+" "]) {
			fmt.Fprintf(&report, "    %s", line)
		}
		if timeout := verdict.timeouts[failure]; len(timeout) > 0 {
			report.WriteString("    its test binary timed out, which printed:\n")
			for _, line := range timeout {
				fmt.Fprintf(&report, "      %s", line)
			}
		}
	}
	return report.String()
}

func describeEnd(runError error) string {
	if runError == nil {
		return "with exit 0"
	}
	return runError.Error()
}

// runModuleTests runs the module's tests for the gate: on a gate box, or as a land-gate job, whose no verdict refuses
// the landing and never falls back to Kirk's Mac, or on Kirk's Mac alone when gateVariable says gateOffSetting, the
// -race run included. It returns the exit code and where they ran, as the land line says it after what was tested:
// "on Sun2 (...)", "on a scratch box (...)" or "on Kirk's Mac, since ...".
func runModuleTests(
	worktree string,
	commit string,
	task string,
	covered project.Workspace,
	testing []string,
) (int, string) {
	patterns := covered.Patterns()
	races := racePatterns(worktree)
	setting := os.Getenv(gateVariable)
	if setting == gateOffSetting {
		return runModuleTestsHere(worktree, testing, patterns, races)
	}
	verdict, unavailable := runBoxGate(worktree, commit, task, patterns, races, setting)
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no verdict from a gate box, so nothing landed, and its tests never "+
			"fall back to Kirk's Mac: %s\n", unavailable)
		return 1, ""
	}
	if verdict.refused != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s refused to test this landing, so nothing landed: %s\n",
			verdict.box, verdict.refused)
		return 1, ""
	}
	platform := verdict.operatingSystem + "/" + verdict.architecture
	if verdict.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s failed, so nothing landed:\n", verdict.box,
			platform)
		fmt.Fprint(streams.Errors, verdict.failureReport())
		return 1, ""
	}
	if verdict.race != nil && verdict.race.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run on %s failed, so nothing landed:\n", verdict.box,
			platform)
		fmt.Fprint(streams.Errors, verdict.race.failureReport())
		printRaceReports(verdict.race.raceReports)
		return 1, ""
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s passed, %d packages\n", verdict.box, platform,
		verdict.packagesPassed)
	where := fmt.Sprintf("on %s (%s, %d packages, %s)", verdict.box, platform, verdict.packagesPassed, sharedBoxTrust)
	if verdict.job != "" {
		where = fmt.Sprintf("on a scratch box (%s, %d packages, %s; land-gate job %s)", platform,
			verdict.packagesPassed, sharedBoxTrust, verdict.job)
	}
	if verdict.race != nil {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run passed, %d packages\n", verdict.box,
			verdict.race.packagesPassed)
		where += fmt.Sprintf(", %d of them again under -race", verdict.race.packagesPassed)
	}
	// A box on Kirk's Mac's own platform compiles every test it does. One on another, a land-gate job's Linux box,
	// never compiles some, which nothing runs since no test falls back to Kirk's Mac, so they are named.
	if verdict.operatingSystem != runtime.GOOS || verdict.architecture != runtime.GOARCH {
		neverCompiled, neverCompiledError := testsNeverCompiledOn(worktree, covered.Modules, runtime.GOOS,
			runtime.GOARCH, verdict.operatingSystem, verdict.architecture)
		if neverCompiledError != nil {
			fmt.Fprintf(streams.Errors, "cohere-dev: %v\n", neverCompiledError)
			return 2, ""
		}
		if named := testsNamed(neverCompiled); len(named) > 0 {
			fmt.Fprintf(streams.Errors, "cohere-dev: %d tests never compiled on %s, so run nowhere: %s\n",
				len(named), platform, strings.Join(named, ", "))
			where += fmt.Sprintf(", %d never compiled on %s and run nowhere: %s", len(named), platform,
				strings.Join(named, ", "))
		}
	}
	skipped := testsNamed(verdict.skipped)
	if len(skipped) == 0 {
		where += ", nothing it skipped"
	} else {
		fmt.Fprintf(streams.Errors, "cohere-dev: %d tests skipped on %s, so run nowhere: %s\n", len(skipped),
			verdict.box, strings.Join(skipped, ", "))
		where += fmt.Sprintf(", %d skipped on %s and run nowhere: %s", len(skipped), verdict.box,
			strings.Join(skipped, ", "))
	}
	if optIn := testsNamed(verdict.optIn); len(optIn) > 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: opt-in, so run by no gate: %s\n", strings.Join(optIn, ", "))
		where += "; opt-in, which no gate runs: " + strings.Join(optIn, ", ")
	}
	if verdict.corpora != "" {
		where += "; the corpora laid at their pins: " + verdict.corpora
	}
	for _, unlaid := range verdict.unlaidCorpora {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", unlaid)
		where += "; " + unlaid
	}
	return 0, where
}

// runModuleTestsHere runs the module's tests on Kirk's Mac, the escape gateOffSetting names, then the -race run once
// the module passes, through the gate's token, and returns the exit code and where they ran. The -race run is
// sharedBoxGate's: one go test of the plain race tokens, then one of each token's package with its run, every one run
// and the first failure's code the run's.
func runModuleTestsHere(worktree string, testing []string, patterns []string, races []string) (int, string) {
	fmt.Fprintf(streams.Errors, "cohere-dev: %s=%s, so the tests run here on Kirk's Mac\n", gateVariable,
		gateOffSetting)
	where := "on Kirk's Mac, since " + gateVariable + "=" + gateOffSetting
	code := runTestsHere(worktree, testing, append([]string{"test"}, patterns...))
	if code != 0 || len(races) == 0 {
		return code, where
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: the module passed, so its %d -race packages run here too\n", len(races))
	plainPackages := []string{}
	runs := [][]string{}
	for _, race := range races {
		if directory, run, hasRun := strings.Cut(race, raceRunSeparator); hasRun {
			runs = append(runs, []string{"test", "-race", "-run", run, directory})
			continue
		}
		plainPackages = append(plainPackages, race)
	}
	// Only when a plain token is there: go test with no package tests the worktree's root, which no token named.
	if len(plainPackages) > 0 {
		runs = slices.Insert(runs, 0, append([]string{"test", "-race"}, plainPackages...))
	}
	raceCode := 0
	for _, arguments := range runs {
		if runCode := runTestsHere(worktree, testing, arguments); runCode != 0 && raceCode == 0 {
			raceCode = runCode
		}
	}
	if raceCode != 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: the -race run here failed, so nothing landed\n")
		return raceCode, ""
	}
	return 0, where + fmt.Sprintf(", then %d packages again under -race", len(races))
}

// printRaceReports prints the first raceReportsShown of a run's race reports whole, and counts the rest, so a refused
// landing shows where each race is without a lander pulling the stacks from the box by hand.
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

// runTestsHere runs a go test on Kirk's Mac, in the gate's environment, writing to the terminal, and returns its exit
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
