package main

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/system-inc/cohere/internal/cache_directory"
	"github.com/system-inc/cohere/internal/types/program"
)

// goInputsLine is an inputs record whose paths are all below root, as the Go engine writes one.
func goInputsLine(root string) string {
	return `{"kind":"inputs","files":["` + filepath.Join(root, "a.go") + `","` + filepath.Join(root, "go.mod") +
		`"],"directories":["` + root + `"],"absent":["` + filepath.Join(filepath.Dir(root), "go.work") + `"]}`
}

// goStreamWith is the Go contract's clean stream with extra records placed before its summary, where the engine
// writes its inputs record.
func goStreamWith(test *testing.T, extra ...string) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	last := len(lines) - 1
	if !strings.HasPrefix(lines[last], `{"kind":"summary"`) {
		test.Fatal("GoClean.jsonl does not end on its summary")
	}
	return append(append(slices.Clone(lines[:last]), extra...), lines[last])
}

// acceptGoStream feeds a Go stream to a fresh run in mode and ends it as an engine exiting 0 would, giving the run
// and the first refusal.
func acceptGoStream(test *testing.T, mode engineMode, lines []string) (*engineRun, error) {
	test.Helper()
	var out bytes.Buffer
	stream := bufio.NewWriter(&out)
	run := newEngineRun(stream, mode, goLanguage("module"), "", time.Now())
	for _, line := range lines {
		if acceptError := run.accept([]byte(line)); acceptError != nil {
			return run, acceptError
		}
	}
	_, finishError := run.finish(0, "exited 0")
	return run, finishError
}

// TestEngineRunKeepsTheGoInputsRecord: a check run's inputs record is kept on the run whole, which is what the run
// cache records a Go run by, and a stream that breaks the record's rules is refused: a second record, which would
// leave one of two proofs unread, a relative path, which the front door would stat from the wrong directory, a
// record naming no file, which would match any tree, and an inputs record in a listing, which is no verdict.
// Not parallel: it renders check runs through engineRun, whose record acceptors set the package-level activeSummary.
func TestEngineRunKeepsTheGoInputsRecord(test *testing.T) {
	root := filepath.Join(test.TempDir(), "module")
	run, acceptError := acceptGoStream(test, engineModeCheck, goStreamWith(test, goInputsLine(root)))
	if acceptError != nil {
		test.Fatalf("a clean stream with an inputs record was refused: %v", acceptError)
	}
	if run.inputs == nil {
		test.Fatal("the inputs record was accepted and not kept")
	}
	if !slices.Equal(run.inputs.Files, []string{filepath.Join(root, "a.go"), filepath.Join(root, "go.mod")}) ||
		!slices.Equal(run.inputs.Directories, []string{root}) ||
		!slices.Equal(run.inputs.Absent, []string{filepath.Join(filepath.Dir(root), "go.work")}) {
		test.Errorf("the inputs kept are %+v", *run.inputs)
	}

	refusals := []struct {
		name    string
		mode    engineMode
		lines   []string
		refusal string
	}{
		{"a second inputs record", engineModeCheck,
			goStreamWith(test, goInputsLine(root), goInputsLine(root)), "a second inputs record"},
		{"a relative file", engineModeCheck,
			goStreamWith(test, `{"kind":"inputs","files":["a.go"],"directories":[],"absent":[]}`),
			`naming "a.go", which is not an absolute path`},
		{"a relative directory", engineModeCheck,
			goStreamWith(test, `{"kind":"inputs","files":["/m/a.go"],"directories":["m"],"absent":[]}`),
			`naming "m", which is not an absolute path`},
		{"a relative absent path", engineModeCheck,
			goStreamWith(test, `{"kind":"inputs","files":["/m/a.go"],"directories":[],"absent":["go.work"]}`),
			`naming "go.work", which is not an absolute path`},
		{"no file", engineModeCheck,
			goStreamWith(test, `{"kind":"inputs","files":[],"directories":["/m"],"absent":[]}`),
			"an inputs record naming no file"},
		{"an inputs record in a version listing", engineModeVersion,
			[]string{contractFixture(test, "GoClean.jsonl")[0], goInputsLine(root)},
			"an inputs record in a Version run"},
	}
	for _, refused := range refusals {
		// Not parallel: it renders check runs through engineRun, whose record acceptors set activeSummary.
		test.Run(refused.name, func(test *testing.T) {
			_, refusalError := acceptGoStream(test, refused.mode, refused.lines)
			if refusalError == nil || !strings.Contains(refusalError.Error(), refused.refusal) {
				test.Fatalf("refusal %v, want one saying %q", refusalError, refused.refusal)
			}
		})
	}
}

// TestGoRunCacheFactsMoveWithWhatALoadReads: every fact a Go load depends on that is no file moves the run's key,
// and a variable no load reads, or the same environment in another order, leaves it where it was. A key that
// stayed put under a moved fact would replay a verdict about another build: the tags of `GOFLAGS`, the target, cgo
// and the toolchain each decide which files go loads.
func TestGoRunCacheFactsMoveWithWhatALoadReads(test *testing.T) {
	test.Parallel()
	root := test.TempDir()
	anchor := program.NewPathAnchor(root)
	settingsFile := filepath.Join(root, "CohereSettings.json")
	toolchains := test.TempDir()
	goPath := func(name string) string {
		path := filepath.Join(toolchains, name)
		if writeError := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); writeError != nil {
			test.Fatal(writeError)
		}
		return path
	}
	firstGo, secondGo := goPath("first-go"), goPath("second-go")
	link := filepath.Join(toolchains, "go")
	if linkError := os.Symlink(firstGo, link); linkError != nil {
		test.Fatal(linkError)
	}
	found := func(path string) func(string) (string, error) {
		return func(string) (string, error) { return path, nil }
	}
	base := []string{
		"PATH=/usr/bin", "GOFLAGS=-trimpath", "GOOS=darwin", "CGO_ENABLED=1", "HOME=/home/someone", "EDITOR=vi",
	}
	key := func(environment []string, lookPath func(string) (string, error)) string {
		test.Helper()
		parts, partsError := program.RunCacheKeyPartsOf(nil, anchor.Resolved(),
			goRunCacheFacts(anchor, settingsFile, false, environment, lookPath)...)
		if partsError != nil {
			test.Fatal(partsError)
		}
		return parts.Key()
	}
	replaced := func(name string, value string) []string {
		environment := []string{}
		for _, variable := range base {
			if !strings.HasPrefix(variable, name+"=") {
				environment = append(environment, variable)
			}
		}
		return append(environment, name+"="+value)
	}
	baseline := key(base, found(link))

	moves := []struct {
		name        string
		environment []string
		lookPath    func(string) (string, error)
	}{
		{"GOFLAGS gained a build tag", replaced("GOFLAGS", "-trimpath -tags=replaytest"), found(link)},
		{"GOOS changed", replaced("GOOS", "linux"), found(link)},
		{"GOARCH set", append(slices.Clone(base), "GOARCH=amd64"), found(link)},
		{"CGO_ENABLED changed", replaced("CGO_ENABLED", "0"), found(link)},
		{"GOWORK set", append(slices.Clone(base), "GOWORK=off"), found(link)},
		{"HOME changed", replaced("HOME", "/home/another"), found(link)},
		{"CC set", append(slices.Clone(base), "CC=clang"), found(link)},
		{"CXX set", append(slices.Clone(base), "CXX=clang++"), found(link)},
		{"PKG_CONFIG set", append(slices.Clone(base), "PKG_CONFIG=/opt/bin/pkg-config"), found(link)},
		{"BURNTSUSHI_TOML_110 set", append(slices.Clone(base), "BURNTSUSHI_TOML_110=1"), found(link)},
		{"another go found on PATH", base, found(secondGo)},
		{"no go found on PATH", base, func(string) (string, error) { return "", os.ErrNotExist }},
	}
	for _, moved := range moves {
		if key(moved.environment, moved.lookPath) == baseline {
			test.Errorf("%s and the key stayed put", moved.name)
		}
	}

	// The go PATH finds is a link the toolchain's installer moves, and only where it leads changed.
	if removeError := os.Remove(link); removeError != nil {
		test.Fatal(removeError)
	}
	if linkError := os.Symlink(secondGo, link); linkError != nil {
		test.Fatal(linkError)
	}
	if key(base, found(link)) == baseline {
		test.Error("the go on PATH was relinked to another toolchain and the key stayed put")
	}
	if removeError := os.Remove(link); removeError != nil {
		test.Fatal(removeError)
	}
	if linkError := os.Symlink(firstGo, link); linkError != nil {
		test.Fatal(linkError)
	}

	reordered := slices.Clone(base)
	slices.Reverse(reordered)
	stays := []struct {
		name        string
		environment []string
	}{
		{"a variable no load reads changed", replaced("EDITOR", "nano")},
		{"PATH changed and finds the same go", replaced("PATH", "/opt/bin:/usr/bin")},
		{"the same environment in another order", reordered},
	}
	for _, stayed := range stays {
		if key(stayed.environment, found(link)) != baseline {
			test.Errorf("%s and the key moved, so an unchanged load would never replay", stayed.name)
		}
	}
}

// TestGoRunUnrecordableKeepsAllButAVerdict: only exit 0 or 1 from a run that called itself complete and rewrote
// nothing is recorded. A run that fell short, ended as no verdict, or rewrote the tree it read is checked again.
func TestGoRunUnrecordableKeepsAllButAVerdict(test *testing.T) {
	test.Parallel()
	ended := func(complete bool, exitCode int) *engineRun {
		summary := engineSummaryRecord{
			Findings: new(int), Complete: new(bool), ExitCode: new(int),
		}
		*summary.Findings, *summary.Complete, *summary.ExitCode = exitCode, complete, exitCode
		return &engineRun{summary: &summary}
	}
	rewrote := ended(true, 0)
	rewrote.filesRewritten = 2
	// Complete, since every file was checked, but go list ran beside a go.work.sum with no side copy (#34r3v1y).
	unguarded := ended(true, 0)
	unguarded.summary.UnguardedWorkspaceSums = []string{"/work/go.work.sum"}
	cases := []struct {
		name     string
		run      *engineRun
		exitCode int
		reason   string
	}{
		{"a clean verdict", ended(true, 0), 0, ""},
		{"a failing verdict", ended(true, 1), 1, ""},
		{"a run that did not run", ended(true, 0), 2, "the run exited 2"},
		{"a run that fell short", ended(false, 1), 1, "did not check everything"},
		{"a run with no summary", &engineRun{}, 0, "without a summary"},
		{"a run whose fix phase rewrote files", rewrote, 0, "the fix phase rewrote 2 files"},
		{"a run whose go list may have rewritten a go.work.sum", unguarded, 0,
			"go list may have rewritten 1 go.work.sum of the tree"},
	}
	for _, testCase := range cases {
		reason := engineRunUnrecordable(testCase.run, testCase.exitCode)
		wrong := reason != ""
		if testCase.reason != "" {
			wrong = !strings.Contains(reason, testCase.reason)
		}
		if wrong {
			test.Errorf("%s: the reason is %q, want %q", testCase.name, reason, testCase.reason)
		}
	}
}

// TestARenamingGoRunIsNeverRecorded: the stream the Go engine writes for a run that renamed across files, read from the
// engine's own golden for that case, is accepted whole, and the run is declined for the record, since its fix phase
// rewrote the tree it read. A rename writes after the load, so the guard must hold for the files it wrote too, which
// the fix record counts with the formatter's.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary.
func TestARenamingGoRunIsNeverRecorded(test *testing.T) {
	golden := filepath.Join("..", "..", "internal", "go_engine", "testdata", "rename-rewritten", "expected.jsonl")
	contents, readError := os.ReadFile(golden)
	if readError != nil {
		test.Fatal(readError)
	}
	var out bytes.Buffer
	run := newEngineRun(bufio.NewWriter(&out), engineModeCheck, goLanguage("module"), "", time.Now())
	for line := range strings.Lines(string(contents)) {
		if acceptError := run.accept([]byte(strings.TrimSuffix(line, "\n"))); acceptError != nil {
			test.Fatalf("the renaming run's stream was refused: %v", acceptError)
		}
	}
	exitCode, finishError := run.finish(1, "exited 1")
	if finishError != nil || exitCode != 1 {
		test.Fatalf("the renaming run finished %d, %v, want 1", exitCode, finishError)
	}
	if run.filesRewritten == 0 {
		test.Fatal("the golden stream rewrote no file, so it is no renaming run")
	}
	if reason := engineRunUnrecordable(run, exitCode); !strings.Contains(reason, "the fix phase rewrote") {
		test.Fatalf("a renaming run would be recorded: the reason is %q", reason)
	}
}

// TestDeclareGoRunCacheInputsDeclaresTheEnginesAndTheFrontDoors: a verdict is declared with the engine's files,
// directories and absent paths, and the settings file the front door handed the engine, present or absent. A run
// whose engine wrote no inputs record is never declared, and one that is no verdict is declined.
// Not parallel: it sets the package-level activeRunCache for its length.
func TestDeclareGoRunCacheInputsDeclaresTheEnginesAndTheFrontDoors(test *testing.T) {
	previous := activeRunCache
	defer func() { activeRunCache = previous }()
	root := test.TempDir()
	present := filepath.Join(root, "CohereSettings.json")
	if writeError := os.WriteFile(present, []byte(`{"go":{"rules":{}}}`), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	absent := filepath.Join(root, "Missing.json")
	complete, exitCode, findings := true, 0, 0
	declaring := func() *engineRun {
		return &engineRun{
			summary: &engineSummaryRecord{Findings: &findings, Complete: &complete, ExitCode: &exitCode},
			inputs: &engineInputsRecord{
				Files:       []string{filepath.Join(root, "a.go")},
				Directories: []string{root},
				Absent:      []string{filepath.Join(filepath.Dir(root), "go.work")},
			},
		}
	}

	for _, settingsFile := range []string{present, absent} {
		activeRunCache = &runCacheSession{}
		declareGoRunCacheInputs(declaring(), 0, settingsFile)
		session := activeRunCache
		if !session.declared || session.declined != "" {
			test.Fatalf("a verdict with inputs was not declared (declined %q)", session.declined)
		}
		wantFiles := []string{filepath.Join(root, "a.go")}
		wantAbsent := []string{filepath.Join(filepath.Dir(root), "go.work")}
		if settingsFile == present {
			wantFiles = append(wantFiles, present)
		} else {
			wantAbsent = append(wantAbsent, absent)
		}
		if !slices.Equal(session.extraFiles, wantFiles) || !slices.Equal(session.extraDirectories, []string{root}) ||
			!slices.Equal(session.extraAbsent, wantAbsent) {
			test.Errorf("with %s declared files %q, directories %q, absent %q; want %q, [%s], %q",
				filepath.Base(settingsFile), session.extraFiles, session.extraDirectories, session.extraAbsent,
				wantFiles, root, wantAbsent)
		}
	}

	activeRunCache = &runCacheSession{}
	withoutInputs := declaring()
	withoutInputs.inputs = nil
	declareGoRunCacheInputs(withoutInputs, 0, present)
	if activeRunCache.declared || len(activeRunCache.extraFiles) > 0 {
		test.Error("a run whose engine wrote no inputs record was declared, so it would be recorded with no proof")
	}

	activeRunCache = &runCacheSession{}
	declareGoRunCacheInputs(declaring(), 2, present)
	if activeRunCache.declared || activeRunCache.declined == "" {
		test.Error("a run that exited 2 was declared rather than declined")
	}

	// A record naming the cache the run writes is declined, and said beside the table for the next run.
	activeRunCache = &runCacheSession{directory: test.TempDir(), projectRoots: []string{root}}
	cached := declaring()
	cached.inputs.Files = append(cached.inputs.Files, filepath.Join(cache_directory.Of(root), "go", "entry"))
	declareGoRunCacheInputs(cached, 0, present)
	if activeRunCache.declared || !strings.Contains(activeRunCache.declined, "inside the cache directory") {
		test.Errorf("a record naming the cache directory was not declined (declined %q)", activeRunCache.declined)
	}
}

// TestGoInputsUnreplayableRefusesWhatWouldNeverReplay: an inputs record naming a path inside a root's cache
// directory, which the run writes after its record is taken, or listing a directory above the root, whose listing
// moves with everything beside the project, is refused, since either would make every later run a miss that only a
// cold run every time would show. A record of the module's own paths, and of files above it, is kept.
func TestGoInputsUnreplayableRefusesWhatWouldNeverReplay(test *testing.T) {
	test.Parallel()
	root := "/work/module"
	roots := []string{root, "/private/work/module"}
	cases := []struct {
		name   string
		inputs engineInputsRecord
		reason string
	}{
		{"the module's own paths, and a file above it", engineInputsRecord{
			Files:       []string{"/work/module/a.go", "/work/staticcheck.conf"},
			Directories: []string{root, "/work/module/sub"},
			Absent:      []string{"/work/go.work"},
		}, ""},
		{"a file in the engine's cache", engineInputsRecord{
			Files: []string{"/work/module/a.go", "/work/module/.cache/cohere/go/entry"},
		}, "inside the cache directory"},
		{"the cache directory listed, under the resolved root", engineInputsRecord{
			Files: []string{"/work/module/a.go"}, Directories: []string{"/private/work/module/.cache/cohere"},
		}, "inside the cache directory"},
		{"an absent path in the cache", engineInputsRecord{
			Files: []string{"/work/module/a.go"}, Absent: []string{"/work/module/.cache/cohere/run"},
		}, "inside the cache directory"},
		{"a directory above the root listed", engineInputsRecord{
			Files: []string{"/work/module/a.go"}, Directories: []string{"/work"},
		}, "a directory above the root"},
	}
	for _, testCase := range cases {
		reason := engineInputsUnreplayable("the Go engine", &testCase.inputs, roots, "")
		wrong := reason != ""
		if testCase.reason != "" {
			wrong = !strings.Contains(reason, testCase.reason)
		}
		if wrong {
			test.Errorf("%s: the reason is %q, want %q", testCase.name, reason, testCase.reason)
		}
	}
}

// goStreamWithLintField is the Go contract's clean stream with one more field on its lint record, as the engine writes
// its `cache` object and its `builds`.
func goStreamWithLintField(test *testing.T, name, value string) []string {
	test.Helper()
	lines := contractFixture(test, "GoClean.jsonl")
	for index, line := range lines {
		if strings.HasPrefix(line, `{"kind":"lint"`) {
			lines[index] = strings.TrimSuffix(line, "}") + `,"` + name + `":` + value + "}"
			return lines
		}
	}
	test.Fatal("GoClean.jsonl holds no lint record")
	return nil
}

// TestAGoRunSaysWhatItsPackageCacheDid: a Go lint record's `cache` object is one line of the --verbose account, its
// two rarer counts only when they are not zero and `off` under `--no-cache`, and a field of the `--json` summary of
// its own; a record without it, as every Swift record is, prints neither, so a Swift run's output is as it was.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestAGoRunSaysWhatItsPackageCacheDid(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	cases := []struct {
		name        string
		lines       []string
		line        string
		summaryJson string
	}{
		{"a warm run", goStreamWithLintField(test, "cache",
			`{"on":true,"hit":412,"missed":3,"written":3,"unstorable":0,"factsDropped":0,"factsSkipped":0,"waves":1}`),
			"  lint: package cache hit 412, missed 3, wrote 3\n",
			`"packageCache":{"on":true,"hit":412,"missed":3,"written":3,"unstorable":0,"factsDropped":0,` +
				`"factsSkipped":0,"waves":1}`},
		{"a run with unstorable packages and dropped and skipped facts", goStreamWithLintField(test, "cache",
			`{"on":true,"hit":1,"missed":4,"written":2,"unstorable":2,"factsDropped":7,"factsSkipped":5,"waves":2}`),
			"  lint: package cache hit 1, missed 4, wrote 2, 2 unstorable, 7 facts dropped, 5 facts skipped\n",
			`"packageCache":{"on":true,"hit":1,"missed":4,"written":2,"unstorable":2,"factsDropped":7,` +
				`"factsSkipped":5,"waves":2}`},
		{"a run under --no-cache", goStreamWithLintField(test, "cache",
			`{"on":false,"hit":0,"missed":0,"written":0,"unstorable":0,"factsDropped":0,"factsSkipped":0,"waves":0}`),
			"  lint: package cache off\n",
			`"packageCache":{"on":false,"hit":0,"missed":0,"written":0,"unstorable":0,"factsDropped":0,` +
				`"factsSkipped":0,"waves":0}`},
		{"a record with no cache object", contractFixture(test, "GoClean.jsonl"), "", ""},
	}
	for _, testCase := range cases {
		verbose, _ := renderGoStreamAs(test, outputVerbose, testCase.lines)
		said := regexp.MustCompile(`(?m)^  lint: package cache.*\n`).FindString(verbose)
		if said != testCase.line {
			test.Errorf("%s: the account says %q, want %q:\n%s", testCase.name, said, testCase.line, verbose)
		}
		asJson, _ := renderGoStreamAs(test, outputJson, testCase.lines)
		summaryLine := summaryJsonOf(test, asJson)
		wrong := strings.Contains(summaryLine, "packageCache")
		if testCase.summaryJson != "" {
			wrong = !strings.Contains(summaryLine, testCase.summaryJson)
		}
		if wrong {
			test.Errorf("%s: the summary is %s, want %q in it", testCase.name, summaryLine, testCase.summaryJson)
		}
	}

	// Swift's records never carry the object, so neither view of a Swift run gains anything.
	activeOutput, activeSummary = outputSettings{Mode: outputVerbose}, runSummary{}
	swift, _, renderError := renderRecords(test, engineModeCheck, contractFixture(test, "Clean.jsonl"), 0)
	if renderError != nil {
		test.Fatal(renderError)
	}
	if strings.Contains(swift, "package cache") || activeSummary.PackageCache != nil {
		test.Errorf("a Swift run said a package cache:\n%s", swift)
	}
}

// goBuildsObject is a lint record's `builds` as the Go engine writes it on a Mac whose project has files only Windows
// compiles: the host's build, which the merge dropped two findings of, and Windows', which added six and lost one.
const goBuildsObject = `[` +
	`{"target":"darwin/arm64 cgo","host":true,"compiledFiles":412,"analyzedPackages":349,"findingsAdded":0,` +
	`"findingsDropped":2},` +
	`{"target":"windows/arm64","host":false,"compiledFiles":1398,"analyzedPackages":349,"findingsAdded":6,` +
	`"findingsDropped":1}]`

// goBuildsLines is the account's line for each build of goBuildsObject.
const goBuildsLines = "  lint: the host's build, darwin/arm64 cgo: 412 files compiled, 349 packages analyzed, " +
	"2 findings dropped by the merge\n" +
	"  lint: build windows/arm64: 1,398 files compiled, 349 packages analyzed, 6 findings added, 1 dropped by the " +
	"merge\n"

// goBuildsJson is goBuildsObject as the `--json` summary writes it.
const goBuildsJson = goBuildsObject

// goCleanLintLine is the lint line of the Go contract's clean stream, which the builds' lines follow.
const goCleanLintLine = "lint: 0 findings \u2014 5 rules over 3 files, 1408 nodes visited, walked by the fix phase " +
	"(nothing was rewritten, so its findings still hold)\n"

// measuredTotalLine stands for the account's total line, whose total is how long the render took, so it and the
// remainder against the 354ms the module and the phases account for move with the machine's load: a loaded run read
// `total 1ms` and failed (#wq0c0sv). What the code guarantees is the module's 96ms, the phases' 258ms, and the
// relation between the two durations the line measures, so the test pins those and never the time it measured.
const measuredTotalLine = "  total (measured): module 96ms, phases 258ms"

// accountedTotal is what the module and the phases account for in the account's total line, the 96ms and the 258ms.
const accountedTotal = 354 * time.Millisecond

// measuredTotal reads the account's total line in either of its shapes: the phases overlapping the total while it is
// under accountedTotal, and the rest outside any phase once it is over. It captures the total and the remainder.
var measuredTotal = regexp.MustCompile(`(?m)^  total ([0-9][0-9.hmsµ]*): module 96ms` +
	`(?: plus phases, overlapping by ([0-9][0-9.hmsµ]*)|, phases 258ms, ([0-9][0-9.hmsµ]*) outside any phase)$`)

// pinMeasuredTotal is an account with its total line written as measuredTotalLine when the line's two durations keep
// the relation the render computes: the total and the overlap add up to accountedTotal, or the total less what sits
// outside any phase does. Each is rounded to the millisecond on its own, so the two may miss it by one. Any other
// line is left as it was, so it still differs from the want: one naming another module time, and one in shape whose
// arithmetic is wrong.
func pinMeasuredTotal(account string) string {
	return measuredTotal.ReplaceAllStringFunc(account, func(line string) string {
		parts := measuredTotal.FindStringSubmatch(line)
		total, totalError := time.ParseDuration(parts[1])
		if totalError != nil {
			return line
		}
		accounted := total
		if parts[2] != "" {
			overlap, overlapError := time.ParseDuration(parts[2])
			if overlapError != nil {
				return line
			}
			accounted += overlap
		} else {
			outside, outsideError := time.ParseDuration(parts[3])
			if outsideError != nil {
				return line
			}
			accounted -= outside
		}
		if difference := accounted - accountedTotal; difference < -time.Millisecond || difference > time.Millisecond {
			return line
		}
		return measuredTotalLine
	})
}

// goCleanAccount is the --verbose account of the Go contract's clean stream, its footer aside, as it read before the
// lint record's `builds` was read, written out so anything added to it shows here.
const goCleanAccount = "module described in 96ms: 4 Go files in the module, 3 of them ours\n" +
	"fix: 0 of 3 files rewritten, 0 fixes applied, 0 refused\n" +
	"format scope: changed files (working tree against HEAD): 0\n" +
	"types: 0 diagnostics over 3 files in 240ms\n" +
	goCleanLintLine +
	"coverage: 5 rules = 5 ran and found nothing\n" +
	"  0 files crashed \u00b7 details: cohere --coverage\n" +
	"  not checked: /project/internal/schema/schema_generated.go (ignored by git)\n" +
	"phases: fix ran in 18ms \u00b7 types ran in 240ms \u00b7 " +
	"lint reused the fix phase's walk (nothing was rewritten) \u00b7 unused skipped (not requested)\n" +
	measuredTotalLine + "\n" +
	"  memory: the collector runs at Go's default, since this is a test binary\n"

// TestTheMeasuredTotalLineIsPinnedByItsShape: either shape the total line takes, by how long the render took, reads
// as measuredTotalLine while its durations keep their relation, within the millisecond rounding can cost, and a line
// naming another module time, or in shape with arithmetic that is wrong, is left as it was, so it still differs from
// the want.
func TestTheMeasuredTotalLineIsPinnedByItsShape(test *testing.T) {
	test.Parallel()
	for _, testCase := range []struct {
		line   string
		pinned bool
	}{
		{line: "  total 0s: module 96ms plus phases, overlapping by 354ms", pinned: true},
		{line: "  total 1ms: module 96ms plus phases, overlapping by 353ms", pinned: true},
		{line: "  total 1.204s: module 96ms, phases 258ms, 850ms outside any phase", pinned: true},
		{line: "  total 1ms: module 96ms plus phases, overlapping by 354ms", pinned: true},
		{line: "  total 1ms: module 97ms plus phases, overlapping by 353ms", pinned: false},
		{line: "  total 1.204s: module 96ms, phases 259ms, 849ms outside any phase", pinned: false},
		{line: "  total 1ms: module 96ms plus phases, overlapping by 900ms", pinned: false},
		{line: "  total 1ms: module 96ms plus phases, overlapping by 351ms", pinned: false},
		{line: "  total 1.204s: module 96ms, phases 258ms, 1.204s outside any phase", pinned: false},
	} {
		account := "before\n" + testCase.line + "\nafter\n"
		want := account
		if testCase.pinned {
			want = "before\n" + measuredTotalLine + "\nafter\n"
		}
		if got := pinMeasuredTotal(account); got != want {
			test.Errorf("%q pins to\n%s\nwant\n%s", testCase.line, got, want)
		}
	}
}

// goCleanSummaryJson is the `--json` summary of the Go contract's clean stream with its seconds set to 0, as it read
// before the lint record's `builds` was read, written out so a field added to it shows here.
const goCleanSummaryJson = `{"kind":"summary","schemaVersion":1,"verdict":"pass","seconds":0,"graphSeconds":0.096,` +
	`"rules":5,"phases":[{"name":"fix","outcome":"ran","seconds":0.018},{"name":"types","outcome":"ran",` +
	`"seconds":0.24},{"name":"lint","outcome":"reused","seconds":0,` +
	`"detail":"the fix phase's walk (nothing was rewritten)"},{"name":"unused","outcome":"skipped","seconds":0,` +
	`"detail":"not requested"}],"cache":{"replayed":false,"filesReplayed":0,"off":false},"typeErrors":0,` +
	`"findings":0,"filesFixed":0,"filesFormatted":0,"wouldChange":0,"cohered":0,"filesInScope":3,"checked":3,` +
	`"cached":0,"nodes":1408,"gaps":{"programFiles":0,"crashedFiles":0,"ruleCrashes":0,` +
	`"rulesSkippingEverything":0,"formattingNotChecked":false,"nothingToCheck":false,"modifiedBuild":false}}`

// TestAGoRunSaysWhichBuildsItChecked: a Go lint record's `builds` is one line of the --verbose account per build, the
// host's first, after the lint line and its cache line, a count in the footer's parentheses, and a field of the
// `--json` summary of its own, each entry as the engine wrote it but for the counts of why a build analyzed each
// package, which the front door does not read. A record without it, as a run of one build's is, prints none of them,
// and its account, footer and summary are byte for byte what they were before the field was read (#72n9mq0). The
// time a render took is the one thing that moves, so the footer is read off the summary with no time, and the
// summary's seconds are set to 0, and the account's total line is pinned by its shape and its arithmetic
// (pinMeasuredTotal), from a process started 100ms before the render.
// Not parallel: it swaps the package-level activeOutput and activeSummary, which the record acceptors set
func TestAGoRunSaysWhichBuildsItChecked(test *testing.T) {
	savedOutput, savedSummary := activeOutput, activeSummary
	defer func() { activeOutput, activeSummary = savedOutput, savedSummary }()
	footerLine := regexp.MustCompile(`(?m)^✓ 💎 .*\n`)
	secondsField := regexp.MustCompile(`^(\{"kind":"summary","schemaVersion":1,"verdict":"pass","seconds":)[0-9.e-]+,`)
	for _, testCase := range []struct {
		name    string
		lines   []string
		account string
		footer  string
		verbose string
		summary string
	}{
		{
			name:    "two builds",
			lines:   goStreamWithLintField(test, "builds", goBuildsObject),
			account: strings.Replace(goCleanAccount, goCleanLintLine, goCleanLintLine+goBuildsLines, 1),
			footer:  "✓ 💎 0.000s (5 rules • 3 checked • 2 builds)",
			verbose: "✓ 💎 0.000s (5 rules • 3 checked • 2 builds • 1,408 nodes)",
			summary: strings.Replace(goCleanSummaryJson, `,"typeErrors":`, `,"builds":`+goBuildsJson+`,"typeErrors":`,
				1),
		},
		{
			name:    "no builds",
			lines:   contractFixture(test, "GoClean.jsonl"),
			account: goCleanAccount,
			footer:  "✓ 💎 0.000s (5 rules • 3 checked)",
			verbose: "✓ 💎 0.000s (5 rules • 3 checked • 1,408 nodes)",
			summary: goCleanSummaryJson,
		},
	} {
		// Started 100ms back, so the total line measures one and its overlap is one the render computed: at a total
		// that rounds to 0s, an overlap of a flat 354ms would read the same as the right one.
		verbose, summary := renderGoStreamStartedAt(test, outputVerbose, testCase.lines,
			time.Now().Add(-100*time.Millisecond))
		if account := pinMeasuredTotal(footerLine.ReplaceAllString(verbose, "")); account != testCase.account {
			test.Errorf("%s: the account is\n%s\nwant\n%s", testCase.name, account, testCase.account)
		}
		summary.Total = 0
		if got := footer(summary, plain, footerOptions{}); got != testCase.footer {
			test.Errorf("%s: the footer is %q, want %q", testCase.name, got, testCase.footer)
		}
		if got := footer(summary, plain, footerOptions{Verbose: true}); got != testCase.verbose {
			test.Errorf("%s: the footer under --verbose is %q, want %q", testCase.name, got, testCase.verbose)
		}
		asJson, _ := renderGoStreamAs(test, outputJson, testCase.lines)
		if got := secondsField.ReplaceAllString(summaryJsonOf(test, asJson), "${1}0,"); got != testCase.summary {
			test.Errorf("%s: the summary is\n%s\nwant\n%s", testCase.name, got, testCase.summary)
		}
	}
}
