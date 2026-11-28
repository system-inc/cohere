package main

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/system-inc/cohere/internal/command_line/streams"
	"github.com/system-inc/cohere/internal/go_language/project"
	"github.com/system-inc/cohere/internal/testing/needs"
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

// boxStepMarker opens each line a gate box's run prints when a step of it ends, `<step> <seconds>`, so a landing says
// what its gate's time went to (#bc8nf0r).
const boxStepMarker = "cohere-dev-gate: step "

// boxStepBeside ends a step's line when the step ran beside others (#df7s7ej): its seconds are its own, said so the
// account shows where each job's time went, and never added to the run's wall, which the step named beside, the
// stage's own wall, counts once.
const boxStepBeside = "beside"

// slowestPackagesShown is how many of a run's slowest test binaries the land line names, each with its time.
const slowestPackagesShown = 8

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
	// timeout read as a bare `FAIL` with no test named (internal/go_language on Sun2, Nov 23, #r28e4nh).
	timeouts map[string][]string
	// steps is each step the box's run said it ended, `<step> <seconds>`, in order, and stepsWall their sum, the run's
	// wall from its first step's start; packageSeconds each package's test binary's run, from its pass or fail.
	steps          []string
	stepsWall      time.Duration
	packageSeconds map[string]float64
	// budget is the core budget the box's run said it kept to (boxBudgetMarker), empty when it kept to none.
	budget string
	// box names who ran the tests, as the gate's lines say it: the Mac the gap ran on, the land-gate job Loom placed,
	// or the stand-in. It is the road's to set, since the box's own output never names it.
	box string
	// job is the land-gate job whose box ran the tests, empty when a gate box or a stand-in did.
	job string
	// packagesReported is every package the run ended, by import path, in order: each pass, each fail and each
	// package-level skip, a package with no test files. A split gate holds each half's to exactly the packages it was
	// given (gate_split.go).
	packagesReported []string
	// parts is a split gate's two halves' verdicts, each under its box's name, which the merged verdict sums; empty for
	// a gate one box ran whole. halves says on the land line which box ran which half, empty when parts is.
	parts  []boxVerdict
	halves string
	// split is how a split gate's halves were placed and what each took, as the land line says it (splitBalance),
	// empty for a gate one box ran whole.
	split string
	// wholeBecause is why a gate that would have split ran whole on one box, as the land line says it, empty when it
	// split or was never to.
	wholeBecause string
	// shardedRuns is what the run said of each package it ran in shards (sharded_run.go), by import path, and
	// shardedPackages those packages in the order their listings were said. shardCounts is each one's shards, for the
	// timing report, and shardGaps every way their shards failed to run each listed test exactly once.
	shardedRuns     map[string]*shardedRun
	shardedPackages []string
	shardCounts     map[string]int
	shardGaps       []string
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
	// Elapsed is a pass's or a fail's seconds: a package's is its test binary's run, its compile not counted.
	Elapsed float64
}

// readTestJson reads `go test -json` from output, with the box's marker line before it, and anything else it printed
// ignored: a gap prints its own lines around the command's.
func readTestJson(output []byte) boxVerdict {
	verdict := boxVerdict{failedOutput: map[string][]string{}, skipped: map[string][]string{},
		optIn: map[string][]string{}, timeouts: map[string][]string{}, packageSeconds: map[string]float64{},
		shardedRuns: map[string]*shardedRun{}, shardCounts: map[string]int{}}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// reading is whether the stream is inside a race report, from its first line to the rule that ends it.
	reading := false
	// timedOut is each package whose timeout panic the stream is inside, from its panic's line to the package's end.
	timedOut := map[string]bool{}
	// shardPackage and shardIndex are the sharded package and the shard whose output the stream is in, from its
	// marker on, empty and 0 before any.
	shardPackage, shardIndex := "", 0
	for scanner.Scan() {
		line := scanner.Text()
		if why, isRefusal := strings.CutPrefix(line, sharedBoxRefusedMarker); isRefusal {
			verdict.refused = strings.TrimSpace(why)
			continue
		}
		if budget, isBudget := strings.CutPrefix(line, boxBudgetMarker); isBudget {
			verdict.budget = strings.TrimSpace(budget)
			continue
		}
		if step, isStep := strings.CutPrefix(line, boxStepMarker); isStep {
			name, rest, _ := strings.Cut(strings.TrimSpace(step), " ")
			seconds, mark, _ := strings.Cut(rest, " ")
			// Ignored: a step whose seconds don't read is said as 0s, since steps only say where the time went and no
			// verdict comes from them.
			spent, _ := strconv.Atoi(seconds)
			said := name + " " + (time.Duration(spent) * time.Second).String()
			if mark == boxStepBeside {
				verdict.steps = append(verdict.steps, said+" "+boxStepBeside)
				continue
			}
			verdict.steps = append(verdict.steps, said)
			verdict.stepsWall += time.Duration(spent) * time.Second
			continue
		}
		if listing, isListing := strings.CutPrefix(line, boxShardListingMarker); isListing {
			verdict.readShardListing(listing)
			continue
		}
		if shard, isShard := strings.CutPrefix(line, boxShardMarker); isShard {
			shardPackage, shardIndex = verdict.readShardMarker(shard)
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
		// A sharded package's test binary ends once a shard, so its one result is read from them all once the stream
		// ends (finishShardedRuns), and every top-level test's end is kept for the shards' coverage.
		if run := verdict.shardedRuns[event.Package]; run != nil {
			index := 0
			if event.Package == shardPackage {
				index = shardIndex
			}
			if run.readShardEvent(event, index) {
				timedOut[event.Package] = false
				continue
			}
		}
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
			verdict.packageSeconds[event.Package] = event.Elapsed
			verdict.packagesPassed++
			verdict.packagesReported = append(verdict.packagesReported, event.Package)
		case event.Test == "" && event.Action == "fail":
			timedOut[event.Package] = false
			verdict.packageSeconds[event.Package] = event.Elapsed
			verdict.packagesFailed = append(verdict.packagesFailed, event.Package)
			verdict.packagesReported = append(verdict.packagesReported, event.Package)
		case event.Test == "" && event.Action == "skip":
			verdict.packagesReported = append(verdict.packagesReported, event.Package)
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
	verdict.finishShardedRuns()
	return verdict
}

// failed reports whether the verdict holds a failure: a failed package, which a build failure is too, or a test.
func (verdict boxVerdict) failed() bool {
	return len(verdict.packagesFailed) > 0 || len(verdict.failedTests) > 0
}

// halfWall is how long the run took by its steps, its -race run's included, zero when it said none: a split gate's
// half's wall (gate_split.go).
func (verdict boxVerdict) halfWall() time.Duration {
	if verdict.race == nil {
		return verdict.stepsWall
	}
	return verdict.stepsWall + verdict.race.stepsWall
}

// readShardListing reads the line after boxShardListingMarker, a sharded package's listing (sharded_run.go), into
// verdict. A run lists a package once, so a second listing of it is a gap, and the first is the one its shards are held
// to.
func (verdict *boxVerdict) readShardListing(line string) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return
	}
	if verdict.shardedRuns[fields[0]] != nil {
		verdict.shardGaps = append(verdict.shardGaps, fmt.Sprintf("%s's tests were listed twice", fields[0]))
		return
	}
	verdict.shardedRuns[fields[0]] = &shardedRun{listed: fields[1:], shards: map[int][][]string{},
		packageEnds: map[int]int{}, testEnds: map[string][]int{}, walls: map[int]time.Duration{}}
	verdict.shardedPackages = append(verdict.shardedPackages, fields[0])
}

// readShardMarker reads the line after boxShardMarker, a shard's (sharded_run.go), into verdict, and returns the
// package it names and the shard's index, which the events after it belong to. A marker that doesn't read, or names a
// package no listing named, is none: its events read as the stream's, and the shards' coverage finds its tests
// missing. Its wall, `<seconds>s` after the package, is kept for the timing report; no test's name reads as one, so a
// marker without it, a stand-in's, holds its tests from there.
func (verdict *boxVerdict) readShardMarker(line string) (string, int) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[1] != "of" {
		return "", 0
	}
	index, indexError := strconv.Atoi(fields[0])
	count, countError := strconv.Atoi(fields[2])
	run := verdict.shardedRuns[fields[3]]
	if indexError != nil || countError != nil || run == nil {
		return "", 0
	}
	run.count = max(run.count, count)
	names := fields[4:]
	if len(names) > 0 {
		if seconds, isWall := strings.CutSuffix(names[0], "s"); isWall {
			if spent, wallError := strconv.Atoi(seconds); wallError == nil {
				run.walls[index] = time.Duration(spent) * time.Second
				names = names[1:]
			}
		}
	}
	run.shards[index] = append(run.shards[index], names)
	return fields[3], index
}

// finishShardedRuns reads each sharded package's shards as one package result: passed only if every shard's test
// binary passed, its seconds the slowest shard's, reported once. A package whose shards never ended is not reported,
// and its tests' coverage names them. Each package's gaps join the verdict's.
func (verdict *boxVerdict) finishShardedRuns() {
	for _, importPath := range verdict.shardedPackages {
		run := verdict.shardedRuns[importPath]
		verdict.shardGaps = append(verdict.shardGaps, run.coverageGaps(importPath)...)
		ended := 0
		for _, ends := range run.packageEnds {
			ended += ends
		}
		if ended == 0 {
			continue
		}
		verdict.shardCounts[importPath] = run.count
		verdict.packageSeconds[importPath] = run.slowest
		verdict.packagesReported = append(verdict.packagesReported, importPath)
		if run.failed {
			verdict.packagesFailed = append(verdict.packagesFailed, importPath)
			continue
		}
		verdict.packagesPassed++
	}
}

// shardRefusal is why a run's shards give no verdict, every gap named, or empty: when they covered each listing
// exactly once, and when the run refused or is red, since a refusal or a failing test is said first and a red run's
// shards can be short of their listing without the gate being any less red.
func (verdict boxVerdict) shardRefusal() string {
	if len(verdict.shardGaps) == 0 || verdict.refused != "" || verdict.failed() ||
		(verdict.race != nil && verdict.race.failed()) {
		return ""
	}
	return fmt.Sprintf("%s's shards didn't run each listed test exactly once, so its verdict is none: %s",
		verdict.box, strings.Join(verdict.shardGaps, "; "))
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
// setting is what gateVariable says, never gateOffSetting, which runModuleTests keeps on Kirk's Mac. testing is the
// gate's environment, in which a gate on the house Macs lists its packages here, so a whole gate hands its box import
// paths as a split one hands each half (gate_split.go), and the box shards boxShardTable's packages either way
// (sharded_run.go). A run whose shards didn't run each listed test exactly once is no verdict.
func runBoxGate(
	worktree string,
	commit string,
	task string,
	patterns []string,
	races []string,
	setting string,
	testing []string,
) (boxVerdict, string) {
	var output []byte
	var runError error
	box := ""
	job := ""
	// wholeBecause is why a gate that would have split runs whole, empty when it splits or was never to.
	wholeBecause := ""
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
		splitting, splitError := gateSplitSetting(os.Getenv(gateSplitVariable))
		if splitError != nil {
			return boxVerdict{}, splitError.Error()
		}
		pin, unavailable := feedGateCommit(worktree, commit)
		if unavailable != "" {
			return boxVerdict{}, unavailable
		}
		listing, listError := listGatePackages(worktree, patterns, races, testing)
		if listError != nil {
			return boxVerdict{}, fmt.Sprintf("the gate's packages couldn't be listed, so no box was sent them: %v",
				listError)
		}
		if len(listing.packages) == 0 {
			return boxVerdict{}, "the gate's patterns list no package, so no box was sent one"
		}
		if splitting && len(boxes) >= 2 {
			split := runSplitGate(worktree, commit, pin, listing, races, boxes)
			if split.whole == nil {
				return split.verdict, split.unavailable
			}
			boxes, wholeBecause = split.whole, split.because
		}
		output, box, unavailable, runError = runOnGateBoxes(worktree, commit, pin, listing.packages, races, boxes,
			wait)
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
	verdict, unavailable := readBoxRun(output, runError, box, commit, races)
	if unavailable != "" {
		return boxVerdict{}, unavailable
	}
	if refusal := verdict.shardRefusal(); refusal != "" {
		return boxVerdict{}, refusal
	}
	verdict.job = job
	if wholeBecause != "" {
		verdict.wholeBecause = wholeBecause + ", so the whole gate ran on " + box
	}
	return verdict, ""
}

// readBoxRun reads what box's run of commit printed, output, ended as runError says, its module's run and then its
// -race run, asked for races, and returns its verdict, or why it is no verdict, which refuses the landing.
func readBoxRun(output []byte, runError error, box string, commit string, races []string) (boxVerdict, string) {
	moduleOutput, raceOutput, raced := bytes.Cut(output, []byte("\n"+boxRaceMarker+"\n"))
	verdict := readTestJson(moduleOutput)
	verdict.box = box
	if raced {
		race := readTestJson(raceOutput)
		race.box = box
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

// timingReport is where a run's time went, in one line: each step it said, then its slowest test binaries, each by
// import path and seconds, then each sharded package's shards' walls, so the bar on a test binary is read off every
// shard. It is empty when the run said none of them. A split gate's is each half's, under its box's name.
func (verdict boxVerdict) timingReport() string {
	if len(verdict.parts) > 0 {
		halves := []string{}
		for _, part := range verdict.parts {
			if timing := part.timingReport(); timing != "" {
				halves = append(halves, part.box+" "+timing)
			}
		}
		return strings.Join(halves, "; ")
	}
	parts := []string{}
	if verdict.budget != "" {
		parts = append(parts, "within "+verdict.budget)
	}
	if len(verdict.steps) > 0 {
		parts = append(parts, "by step "+strings.Join(verdict.steps, ", "))
	}
	packages := slices.Collect(maps.Keys(verdict.packageSeconds))
	slices.SortFunc(packages, func(one string, other string) int {
		return cmp.Or(cmp.Compare(verdict.packageSeconds[other], verdict.packageSeconds[one]), cmp.Compare(one, other))
	})
	slowest := []string{}
	for _, named := range packages[:min(slowestPackagesShown, len(packages))] {
		// A sharded package's seconds are its slowest shard's, said with how many shards ran it.
		shards := ""
		if count := verdict.shardCounts[named]; count > 0 {
			shards = fmt.Sprintf(" (slowest of %d shards)", count)
		}
		slowest = append(slowest, fmt.Sprintf("%s %.1fs%s", named, verdict.packageSeconds[named], shards))
	}
	if len(slowest) > 0 {
		parts = append(parts, "slowest test binaries "+strings.Join(slowest, ", "))
	}
	for _, importPath := range verdict.shardedPackages {
		if walls := verdict.shardedRuns[importPath].wallsSaid(importPath); walls != "" {
			parts = append(parts, walls)
		}
	}
	return strings.Join(parts, "; ")
}

// skippedOnLanding reads a landing body's count of tests skipped on its gate boxes, its land line's ", N skipped on".
var skippedOnLanding = regexp.MustCompile(`, ([0-9]+) skipped on `)

// previousSkipped is how many tests main's last landing skipped on its gate boxes, read from its body, and -1 when its
// body says no count, a landing from before the gate said one.
func previousSkipped(worktree string) int {
	// Ignored: a main with no landing body to read has no count to compare, which skipJump reads as -1.
	body, _ := gitOutput(worktree, "log", "-1", "--first-parent", "--format=%b", "main")
	if strings.Contains(body, ", nothing it skipped") {
		return 0
	}
	found := skippedOnLanding.FindStringSubmatch(body)
	if found == nil {
		return -1
	}
	// Ignored: the pattern holds only digits.
	count, _ := strconv.Atoi(found[1])
	return count
}

// skipJumpFloor is the least growth in skipped tests skipJump calls a jump, so a handful more on a small count never
// reads as one.
const skipJumpFloor = 20

// skipJump says, loudly, when a gate skipped far more tests than main's last landing did: more than twice as many and
// at least skipJumpFloor more. 196 against 10 printed and changed nothing twice (#3kpxrdt); a jump that large is a
// gate that stopped running something, not a box that lacked a tool. It is empty when there is no jump or no count to
// compare.
func skipJump(now int, before int) string {
	if before < 0 || now <= 2*before || now-before < skipJumpFloor {
		return ""
	}
	return fmt.Sprintf("look before trusting this green: %d tests skipped, against %d at main's last landing, so "+
		"something this gate used to run ran nowhere", now, before)
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

// failedOn names who ran the verdict's failures: its box, or of a split gate each half's box that failed.
func (verdict boxVerdict) failedOn() string {
	if len(verdict.parts) == 0 {
		return verdict.box
	}
	failing := []string{}
	for _, part := range verdict.parts {
		if part.failed() {
			failing = append(failing, part.box)
		}
	}
	return strings.Join(failing, " and ")
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
	verdict, unavailable := runBoxGate(worktree, commit, task, patterns, races, setting, testing)
	if unavailable != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: no verdict from a gate box, so nothing landed, and its tests never "+
			"fall back to Kirk's Mac: %s\n", unavailable)
		return 1, ""
	}
	return verdictLine(worktree, covered, verdict, sharedBoxTrust)
}

// verdictLine says a box's verdict on a landing's tests, read whole (readBoxRun), and returns the exit code and where
// they ran, as the land line says it after what was tested, trust saying what the verdict holds against: a refusal, a
// failing test or a failing -race run refuses the landing, saying why, and a pass is said with its packages, the tests
// it skipped or never compiled and where its time went. The integrate job says its own verdict through it too
// (integrate_job_run.go).
func verdictLine(worktree string, covered project.Workspace, verdict boxVerdict, trust string) (int, string) {
	if verdict.refused != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s refused to test this landing, so nothing landed: %s\n",
			verdict.box, verdict.refused)
		return 1, ""
	}
	platform := verdict.operatingSystem + "/" + verdict.architecture
	// Where the gate's time went, said whatever its verdict, so a slow gate can be read from any landing (#bc8nf0r).
	if timing := verdict.timingReport(); timing != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's gate, %s\n", verdict.box, timing)
	}
	if verdict.race != nil {
		if timing := verdict.race.timingReport(); timing != "" {
			fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run, %s\n", verdict.box, timing)
		}
	}
	// How a split gate's halves were placed beside what each took, so the balance is read from the record (#q1mvjgg).
	if verdict.split != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's gate, %s\n", verdict.box, verdict.split)
	}
	if verdict.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s failed, so nothing landed:\n", verdict.failedOn(),
			platform)
		fmt.Fprint(streams.Errors, verdict.failureReport())
		return 1, ""
	}
	if verdict.race != nil && verdict.race.failed() {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s's -race run on %s failed, so nothing landed:\n",
			verdict.race.failedOn(), platform)
		fmt.Fprint(streams.Errors, verdict.race.failureReport())
		printRaceReports(verdict.race.raceReports)
		return 1, ""
	}
	fmt.Fprintf(streams.Errors, "cohere-dev: %s's tests on %s passed, %d packages\n", verdict.box, platform,
		verdict.packagesPassed)
	where := fmt.Sprintf("on %s (%s, %d packages, %s)", verdict.box, platform, verdict.packagesPassed, trust)
	// A split gate names which box ran which half (gate_split.go).
	if verdict.halves != "" {
		where = fmt.Sprintf("on %s (%s, %d packages: %s, %s)", verdict.box, platform, verdict.packagesPassed,
			verdict.halves, trust)
	}
	if verdict.job != "" {
		where = fmt.Sprintf("on a scratch box (%s, %d packages, %s; land-gate job %s)", platform,
			verdict.packagesPassed, trust, verdict.job)
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
	if jump := skipJump(len(skipped), previousSkipped(worktree)); jump != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", jump)
		where += "; " + jump
	}
	if optIn := testsNamed(verdict.optIn); len(optIn) > 0 {
		fmt.Fprintf(streams.Errors, "cohere-dev: opt-in, so run by no gate: %s\n", strings.Join(optIn, ", "))
		where += "; opt-in, which no gate runs: " + strings.Join(optIn, ", ")
	}
	if verdict.wholeBecause != "" {
		fmt.Fprintf(streams.Errors, "cohere-dev: %s\n", verdict.wholeBecause)
		where += "; " + verdict.wholeBecause
	}
	if verdict.split != "" {
		where += "; " + verdict.split
	}
	// Where the gate's time went joins the landing's body too, so main's history holds it beside the gate's total.
	if timing := verdict.timingReport(); timing != "" {
		where += "; its gate " + timing
	}
	if verdict.race != nil {
		if timing := verdict.race.timingReport(); timing != "" {
			where += "; its -race run " + timing
		}
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
