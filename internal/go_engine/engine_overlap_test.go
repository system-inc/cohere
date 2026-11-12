package go_engine

import (
	"bytes"
	"cmp"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/go_engine/cgo_testing"
	"github.com/system-inc/cohere/internal/go_engine/contract"
	"github.com/system-inc/cohere/internal/go_engine/rules"
)

// overlapRun is one run of the engine over a fresh copy of a fixture: its stream and prose, normalized, and its exit
// code.
type overlapRun struct {
	stream   string
	prose    string
	exitCode int
}

// runOverlapCase copies endToEnd's fixture, prepares it, and runs the engine over it under `--no-cache`, with the lint
// phase beside the load or after it as overlaps says.
func runOverlapCase(test *testing.T, endToEnd endToEndCase, overlaps bool) overlapRun {
	test.Helper()
	root := copyFixture(test, filepath.Join("testdata", cmp.Or(endToEnd.fixture, endToEnd.name)))
	// A case may lock a directory of the copy; every directory is writable again before the copy is removed.
	test.Cleanup(func() {
		// Ignored: a directory left locked fails the copy's removal, which names it, so nothing goes unseen.
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkError error) error {
			if walkError == nil && entry.IsDir() {
				return os.Chmod(path, 0o755)
			}
			return nil
		})
	})
	if endToEnd.prepare != nil {
		if prepareError := endToEnd.prepare(root); prepareError != nil {
			test.Fatalf("preparing the copy: %v", prepareError)
		}
	}
	registered := endToEnd.rules
	if registered == nil {
		registered = rules.All()
	}
	arguments := append([]string{"--contract", fmt.Sprint(contract.Version), "--root", "{root}"}, endToEnd.extra...)
	arguments = append(arguments, "--no-cache")
	for index, argument := range arguments {
		arguments[index] = strings.ReplaceAll(argument, "{root}", root)
	}
	var standardOutput bytes.Buffer
	var standardError bytes.Buffer
	exitCode := runOverlapping(arguments, nil, &standardOutput, &standardError, registered, nil,
		embeddedStandardLibrary, overlaps, nil)
	return overlapRun{
		stream:   normalize(standardOutput.String(), root),
		prose:    strings.ReplaceAll(standardError.String(), root, "/project"),
		exitCode: exitCode,
	}
}

// sameRuns fails unless the run beside the load wrote what the run after it wrote, record for record.
func sameRuns(test *testing.T, overlapped overlapRun, sequential overlapRun) {
	test.Helper()
	if overlapped.exitCode != sequential.exitCode {
		test.Errorf("exit %d beside the load, %d after it", overlapped.exitCode, sequential.exitCode)
	}
	overlappedLines := strings.Split(overlapped.stream, "\n")
	sequentialLines := strings.Split(sequential.stream, "\n")
	for index := range max(len(overlappedLines), len(sequentialLines)) {
		if beside, after := lineAt(overlappedLines, index), lineAt(sequentialLines, index); beside != after {
			test.Errorf("line %d:\nbeside the load %s\nafter it        %s", index+1, beside, after)
		}
	}
	if overlapped.prose != sequential.prose {
		test.Errorf("stderr beside the load:\n%s\nafter it:\n%s", overlapped.prose, sequential.prose)
	}
}

// streamRecord is what these tests read of a record: its kind, a finding's source and file, and a phase's name,
// outcome and detail.
type streamRecord struct {
	Kind    string `json:"kind"`
	Source  string `json:"source"`
	File    string `json:"file"`
	Name    string `json:"name"`
	Outcome string `json:"outcome"`
	Detail  string `json:"detail"`
}

// recordsOfKind are the stream's records of one kind.
func recordsOfKind(test *testing.T, stream string, kind string) []streamRecord {
	test.Helper()
	var found []streamRecord
	for line := range strings.Lines(stream) {
		var record streamRecord
		if decodeError := json.Unmarshal([]byte(line), &record); decodeError != nil {
			test.Fatalf("decoding %q: %v", line, decodeError)
		}
		if record.Kind == kind {
			found = append(found, record)
		}
	}
	return found
}

// A package checked late that does not type-check leaves no trace of the rules that ran beside the load over the
// packages checked before it: late imports early, so the load checks early first, and early's rules may start, and
// find what they find, while late is still checking. The run reads as the run that linted after the load: late's type
// error, the lint phase not reached with main's detail, and no rule's finding, crash or count. That early's rules
// find something is proven by the same tree with late's error fixed, whose run reports it.
func TestTheLintBesideTheLoadLeavesNoTraceWhenALatePackageDoesNotTypeCheck(test *testing.T) {
	test.Parallel()

	broken := endToEndCase{name: "overlap-type-error", extra: []string{"--no-fix"}}
	overlapped := runOverlapCase(test, broken, true)
	sequential := runOverlapCase(test, broken, false)
	sameRuns(test, overlapped, sequential)
	for _, finding := range recordsOfKind(test, overlapped.stream, "finding") {
		if finding.Source != string(contract.SourceCompiler) {
			test.Errorf("want only the compiler's finding, got %+v", finding)
		}
	}
	var lintPhase streamRecord
	for _, phase := range recordsOfKind(test, overlapped.stream, "phase") {
		if phase.Name == string(contract.PhaseLint) {
			lintPhase = phase
		}
	}
	wantDetail := "types bailed: 1 type errors, so lint findings against wrong semantics would be noise"
	if lintPhase.Outcome != string(contract.OutcomeNotReached) || lintPhase.Detail != wantDetail {
		test.Errorf("want the lint phase not reached, %q, got %+v", wantDetail, lintPhase)
	}
	if lintRecords := recordsOfKind(test, overlapped.stream, "lint"); len(lintRecords) > 0 {
		test.Errorf("want no lint record, got %v", lintRecords)
	}

	fixed := broken
	fixed.prepare = func(root string) error {
		path := filepath.Join(root, "late", "late.go")
		contents, readError := os.ReadFile(path)
		if readError != nil {
			return readError
		}
		return os.WriteFile(path, bytes.Replace(contents, []byte(`"text"`), []byte("1"), 1), 0o644)
	}
	early := 0
	for _, finding := range recordsOfKind(test, runOverlapCase(test, fixed, true).stream, "finding") {
		if finding.File == "/project/early/early.go" {
			early++
		}
	}
	if early == 0 {
		test.Errorf("want early's own findings once late type-checks, so the broken run had some to drop, got none")
	}
}

// lateFoldSteps is how many statements the generated late package folds through, enough that the load's parse and type
// check of it take far longer than the rules beside the load take over a package that imports nothing.
const lateFoldSteps = 200000

// A test function go test would refuse is the load's diagnostic however early the rules beside the load finished with
// its test variant. refused imports nothing, so the load checks it and its variant at once, and the rules beside the
// load run over both and let their syntax go while late, generated long enough here that its parse and type check take
// a while, is still loading. The test function is judged as the variant is checked (testFunctionDiagnostics), so the
// run reads as the run that linted after the load: the refused test function, the lint phase not reached, and no
// rule's finding or count. Judged after the load from the variant's syntax, as before the rules ran beside it, the
// function would be read from syntax already let go, missed, and the run would lint.
func TestATestFunctionGoTestRefusesStopsTheLintBesideTheLoad(test *testing.T) {
	test.Parallel()

	refused := endToEndCase{name: "overlap-test-function", extra: []string{"--no-fix"}, prepare: writeLateFold}
	overlapped := runOverlapCase(test, refused, true)
	sequential := runOverlapCase(test, refused, false)
	sameRuns(test, overlapped, sequential)
	findings := recordsOfKind(test, overlapped.stream, "finding")
	if len(findings) != 1 || findings[0].Source != string(contract.SourceCompiler) ||
		findings[0].File != "/project/refused/refused_test.go" {
		test.Errorf("want the one compiler finding of the refused test function, got %+v", findings)
	}
	var lintPhase streamRecord
	for _, phase := range recordsOfKind(test, overlapped.stream, "phase") {
		if phase.Name == string(contract.PhaseLint) {
			lintPhase = phase
		}
	}
	if lintPhase.Outcome != string(contract.OutcomeNotReached) {
		test.Errorf("want the lint phase not reached, got %+v", lintPhase)
	}
	if lintRecords := recordsOfKind(test, overlapped.stream, "lint"); len(lintRecords) > 0 {
		test.Errorf("want no lint record, got %v", lintRecords)
	}
}

// writeLateFold writes the late package of the overlap-test-function fixture into the copy at root: one function of
// lateFoldSteps statements, generated here rather than kept in testdata, since its only job is to be long.
func writeLateFold(root string) error {
	late := filepath.Join(root, "late")
	if makeError := os.MkdirAll(late, 0o755); makeError != nil {
		return makeError
	}
	contents := "// Package late folds a seed through many steps, so the load parses and checks it for a while.\n" +
		"package late\n\n// Fold folds seed.\nfunc Fold(seed uint64) uint64 {\n\tvalue := seed\n" +
		strings.Repeat("\tvalue = value*31 + 7\n", lateFoldSteps) + "\treturn value\n}\n"
	return os.WriteFile(filepath.Join(late, "late.go"), []byte(contents), 0o644)
}

// writtenThroughCount is a build record's count of the entries it wrote through to its own store, which a run with
// the findings cache writes and one under `--no-cache` never does.
var writtenThroughCount = regexp.MustCompile(`,"writtenThrough":[0-9]+`)

// Every end-to-end case writes, under `--no-cache`, exactly what it writes with the lint phase after the load, and
// what its expected stream holds but for the cache being off: a run that lints beside its load, one whose wrap or
// rename writes and so drops or takes that pass, and one whose rules never run.
func TestEveryCaseReadsTheSameWithTheLintBesideTheLoad(test *testing.T) {
	test.Parallel()

	for _, endToEnd := range endToEndCases {
		if endToEnd.arguments != nil || slices.Contains(endToEnd.extra, "--version") {
			continue
		}
		test.Run(endToEnd.name, func(test *testing.T) {
			test.Parallel()
			if endToEnd.needsCgo {
				cgo_testing.RequireTranslation(test)
			}
			overlapped := runOverlapCase(test, endToEnd, true)
			sequential := runOverlapCase(test, endToEnd, false)
			sameRuns(test, overlapped, sequential)
			expected, readError := os.ReadFile(expectedStreamPath(endToEnd.name))
			if readError != nil {
				test.Fatalf("reading the expected stream: %v", readError)
			}
			// The expected stream is of a run with the cache, which a run that renamed has turned off already, and
			// whose extra builds wrote through to their stores what a run without one never writes.
			cacheOff := writtenThroughCount.ReplaceAllString(
				strings.ReplaceAll(string(expected), `"cache":{"on":true`, `"cache":{"on":false`), "")
			expectedLines := strings.Split(cacheOff, "\n")
			actualLines := strings.Split(overlapped.stream, "\n")
			for index := range max(len(expectedLines), len(actualLines)) {
				if want, got := lineAt(expectedLines, index), lineAt(actualLines, index); want != got {
					test.Errorf("expected.jsonl line %d:\nwant %s\ngot  %s", index+1, want, got)
				}
			}
		})
	}
}
