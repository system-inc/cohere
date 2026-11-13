package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/duplicate_functions"
)

// The contract is read from the files swift/Contract.md is made of, the ones the front door's and the
// Swift encoder's tests read, so this side cannot drift from them either.
var (
	contractDocument = filepath.Join("..", "..", "..", "swift", "Contract.md")
	contractFixtures = filepath.Join("..", "..", "..", "swift", "Contract", "*.jsonl")
)

// kindsNotWritten are the contract's record kinds this engine does not write, each with why.
var kindsNotWritten = map[Kind]string{
	"unused":         "the --unused report, which the Go engine refuses until staticcheck's unused is wired",
	"unusedCoverage": "the --unused report's coverage, written only beside unused records",
}

// fieldsNotWritten are contract fields this engine leaves out on purpose, by kind. ProvenanceRecord's
// comment says why for its two, and LintRecord.FilesReplayed's for filesReplayed.
var fieldsNotWritten = map[Kind][]string{
	KindProvenance: {"swiftSyntax", "swiftFormat"},
	KindLint:       {"filesReplayed"},
}

// additiveFields are fields this engine writes beyond the contract, by kind: additive within contract 3, so the
// front door ignores them until it reads them. Each must be written, and none is compared with the contract.
var additiveFields = map[Kind][]string{
	KindFix:  {"notWritten"},
	KindLint: {"withheldBySuppression", "withheldAsGenerated", "withheldAsCopied", "cache", "builds", "offReasons"},
	KindTypes: {
		"withheldInDependencyCgo", "withheldDependencyCgoDeclarations", "cgoTranslation", "censusMissed",
	},
}

// dictionaryFields are objects keyed by data (a reason, a rule name) rather than by field names, so their
// keys are values and are not compared.
var dictionaryFields = map[string]bool{
	"refusalsByReason":      true,
	"notFormattedReasons":   true,
	"fixedBy":               true,
	"rulesScopedOff":        true,
	"filesNotChecked":       true,
	"skipped":               true,
	"offReasons":            true,
	"withheldBySuppression": true,
	"withheldAsGenerated":   true,
	"withheldAsCopied":      true,
}

// shape is a JSON object's field names, each with the shape of its value: nil for a value with no fields,
// and for an array the merged shape of its elements.
type shape map[string]shape

// shapeOf is the shape of one JSON value, read as raw text so each object and array is decoded only as far as
// its members, into types that hold no value of unknown kind. The value was already decoded once whole, so it is
// well formed, and a decode that fails here fails the test rather than reading as a value with no fields.
func shapeOf(test *testing.T, name string, value json.RawMessage) shape {
	test.Helper()
	if dictionaryFields[name] {
		return nil
	}
	trimmed := bytes.TrimSpace(value)
	if len(trimmed) == 0 {
		return nil
	}
	switch trimmed[0] {
	case '{':
		members := map[string]json.RawMessage{}
		if decodeError := json.Unmarshal(trimmed, &members); decodeError != nil {
			test.Fatalf("reading an object's members: %v\n%s", decodeError, trimmed)
		}
		fields := shape{}
		for key, member := range members {
			fields[key] = mergeShapes(fields[key], shapeOf(test, key, member))
		}
		return fields
	case '[':
		var elements []json.RawMessage
		if decodeError := json.Unmarshal(trimmed, &elements); decodeError != nil {
			test.Fatalf("reading an array's elements: %v\n%s", decodeError, trimmed)
		}
		var merged shape
		for _, element := range elements {
			merged = mergeShapes(merged, shapeOf(test, name, element))
		}
		return merged
	}
	return nil
}

// kindOf is a record's kind, and whether it has one written as a string.
func kindOf(record json.RawMessage) (Kind, bool) {
	var envelope struct {
		Kind *string `json:"kind"`
	}
	if decodeError := json.Unmarshal(record, &envelope); decodeError != nil || envelope.Kind == nil {
		return "", false
	}
	return Kind(*envelope.Kind), true
}

// mergeShapes is first with second's fields merged in at every depth, or second when first is nil.
//
// Mutates: first takes second's fields, and is returned, so the caller folds every shape into one map
func mergeShapes(first shape, second shape) shape {
	if first == nil {
		return second
	}
	for key, member := range second {
		first[key] = mergeShapes(first[key], member)
	}
	return first
}

// indexFields records the shape of every field at any depth by its name, so a field whose example in
// one record is empty (a finding's `suggestions: []`) is compared against its example in another (an
// unused record's suggestions).
//
// Mutates: index gains every field of fields, at any depth, merged with the shape it already held for that name
func indexFields(fields shape, index map[string]shape) {
	for key, member := range fields {
		if member != nil {
			index[key] = mergeShapes(index[key], member)
			indexFields(member, index)
		}
	}
}

// contractShapes is every record shape the contract shows, by kind, merged over the JSON examples in
// Contract.md and every line of its fixtures, plus every nested field's shape by name.
func contractShapes(test *testing.T) (map[Kind]shape, map[string]shape) {
	test.Helper()
	objects := []json.RawMessage{}

	document, readError := os.ReadFile(contractDocument)
	if readError != nil {
		test.Fatal(readError)
	}
	inBlock := false
	var block strings.Builder
	for _, line := range strings.Split(string(document), "\n") {
		switch {
		case !inBlock && strings.TrimSpace(line) == "```json":
			inBlock = true
			block.Reset()
		case inBlock && strings.TrimSpace(line) == "```":
			inBlock = false
			decoder := json.NewDecoder(strings.NewReader(block.String()))
			for {
				var object json.RawMessage
				decodeError := decoder.Decode(&object)
				if errors.Is(decodeError, io.EOF) {
					break
				}
				if decodeError != nil {
					test.Fatalf("a JSON example in Contract.md does not parse: %v\n%s", decodeError, block.String())
				}
				objects = append(objects, object)
			}
		case inBlock:
			block.WriteString(line)
			block.WriteString("\n")
		}
	}

	fixtures, globError := filepath.Glob(contractFixtures)
	if globError != nil || len(fixtures) == 0 {
		test.Fatalf("no contract fixtures at %s (%v)", contractFixtures, globError)
	}
	for _, fixture := range fixtures {
		contents, readError := os.ReadFile(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			if !json.Valid([]byte(line)) {
				test.Fatalf("%s: a fixture line is not JSON\n%s", fixture, line)
			}
			objects = append(objects, json.RawMessage(line))
		}
	}

	shapes := map[Kind]shape{}
	fieldIndex := map[string]shape{}
	for _, object := range objects {
		kind, hasKind := kindOf(object)
		if !hasKind {
			test.Fatalf("a contract example has no kind: %s", object)
		}
		fields := shapeOf(test, "", object)
		delete(fields, "kind")
		shapes[kind] = mergeShapes(shapes[kind], fields)
		indexFields(fields, fieldIndex)
	}
	return shapes, fieldIndex
}

// compareShapes reports each field one side has and the other does not, at any depth.
func compareShapes(path string, ours shape, theirs shape, fieldIndex map[string]shape, notWritten []string) []string {
	problems := []string{}
	for key := range ours {
		if _, listed := theirs[key]; !listed {
			problems = append(problems, path+"."+key+" is written and the contract does not list it")
		}
	}
	for key := range theirs {
		_, written := ours[key]
		leftOut := false
		for _, name := range notWritten {
			leftOut = leftOut || name == key
		}
		if written == leftOut {
			if written {
				problems = append(problems, path+"."+key+" is written and is listed as left out on purpose")
			} else {
				problems = append(problems, path+"."+key+" is in the contract and not written")
			}
		}
	}
	for key, member := range ours {
		contractMember, listed := theirs[key]
		if !listed {
			continue
		}
		if contractMember == nil {
			contractMember = fieldIndex[key]
		}
		if member == nil && contractMember == nil {
			continue
		}
		problems = append(problems, compareShapes(path+"."+key, member, contractMember, fieldIndex, nil)...)
	}
	sort.Strings(problems)
	return problems
}

// populatedRecords is one record of each kind with every field set, and every list holding an element
// with every field set, so each field the type can write appears in its encoding.
func populatedRecords() []Record {
	edit := Edit{Start: 30, End: 33, Text: "let"}
	return []Record{
		ProvenanceRecord{
			Contract:           Version,
			Engine:             "cohere-go",
			Version:            "0.1.0",
			Commit:             "dev",
			SourceTreeModified: true,
			Toolchain:          "go1.27",
		},
		ProjectRecord{
			Root:                "/project",
			Package:             "example.com/project",
			ElapsedMilliseconds: 4,
			FilesInPackage:      3,
			FilesOurs:           2,
			FilesInScope:        2,
			ScopeDescription:    "internal/a",
			Targets: []ProjectTarget{
				{Name: "example.com/project", Kind: "module", Files: 3, LanguageMode: "1.27"},
			},
			Excluded: []ExcludedFile{
				{File: "/project/b_windows.go", Reason: "excluded by build constraints on darwin/arm64"},
				{File: "/project/scratch.go", Reason: "ignored by git", Source: ".gitignore:1"},
			},
		},
		UnreadableRecord{File: "/project/c.go", Error: "it could not be opened"},
		FindingRecord{
			Source:      SourceRule,
			File:        "/project/a.go",
			Line:        2,
			Column:      9,
			EndLine:     2,
			EndColumn:   21,
			Severity:    SeverityError,
			Rule:        "cohere-go/printf",
			MessageId:   "printf",
			Message:     "a message",
			Fixes:       []Edit{edit},
			Suggestions: []Suggestion{{Message: "a suggestion", Fixes: []Edit{edit}}},
		},
		FixRecord{
			FilesConsidered:     2,
			FilesRewritten:      1,
			FixesApplied:        1,
			FixesRefused:        1,
			RefusalsByReason:    map[string]int{"overlaps another fix": 1},
			FilesReformatted:    1,
			FilesNotFormatted:   1,
			NotFormattedReasons: map[string]int{"it does not parse": 1},
			FormatScope:         "every file in scope",
			ChangedFiles: []ChangedFile{
				{File: "/project/a.go", FixedBy: map[string]int{"cohere-go/printf": 1}, Formatted: true},
			},
			NotWritten: []NotWrittenFile{{File: "/project/shared/b.go", RealPath: "/elsewhere/shared/b.go"}},
		},
		TypesRecord{
			Diagnostics:         1,
			Files:               2,
			ElapsedMilliseconds: 900,
			FilesWithoutRecord:  []string{"/project/d.go"},
			Build:               "go list -deps -test",
			CensusMissed:        []CensusMissedFile{{Package: "example.com/b", File: "/project/b/b.go"}},
			WithheldInDependencyCgo: []WithheldFile{
				{Package: "net", File: "/goroot/src/net/cgo_unix.go", Errors: 1},
			},
			WithheldDependencyCgoDeclarations: 3,
		},
		LintRecord{
			Findings:              1,
			RulesRun:              4,
			FilesWalked:           2,
			NodesVisited:          640,
			ElapsedMilliseconds:   12,
			ReusedFrom:            "fix",
			RulesSilent:           []string{"cohere-go/copylock"},
			RulesWatchedAndQuiet:  2,
			Crashes:               []Crash{{File: "/project/a.go", Error: "a panic"}},
			RulesScopedOff:        map[string]int{"cohere-go/printf": 1},
			RulesNotConfigured:    []string{"cohere-go/SA4006"},
			ConfigNote:            "a note",
			WithheldBySuppression: map[string]int{"cohere-go/printf": 1},
			WithheldAsGenerated:   map[string]int{"cohere-go/x": 2},
			WithheldAsCopied:      map[string]int{"cohere-go/y": 3},
			Builds: []BuildRecord{
				{
					Target: "darwin/arm64 cgo", Host: true, CompiledFiles: 2, AnalyzedPackages: 1, EntriesKept: 1,
					LoadMilliseconds: 40, LintMilliseconds: 90, KeepMilliseconds: 6, WaitMilliseconds: 3,
				},
				{
					Target: "windows/arm64", CompiledFiles: 2, AnalyzedPackages: 1, FindingsAdded: 1,
					RestoredFromHost: 1, DeclinedAtLoad: 1, DeclinedAtCheck: 1,
					DeclinedBy:   map[string]int{"typeReads": 1},
					ReloadRounds: 1, ReloadRoundsBy: map[string]int{"hostDecline": 1}, WrittenThrough: 1,
					LoadMilliseconds: 30, LintMilliseconds: 50, DecideMilliseconds: 12,
					CheckedFromSource: 3, CheckedFromSourceRounds: []int{1, 2}, PackagesLoaded: 3,
					LintWaitMilliseconds: 8, LintWaited: true, HeldAtLoadEndBytes: 64, HeldAtLintPeakBytes: 128,
					LintPeakOverLoadEnd: 2,
				},
			},
			OffReasons: map[string]string{"cohere-go/printf": "a reason"},
		},
		PhaseRecord{Name: PhaseLint, Outcome: OutcomeRan, ElapsedMilliseconds: 12, Detail: "a detail"},
		RuleRecord{Name: "cohere-go/printf", Severity: "error"},
		DuplicatesRecord{Report: duplicate_functions.Report{
			Ran: true, NotRun: "a reason", Floor: 11, Files: 2, FilesIgnored: 1, Functions: 4, Hashed: 2,
			Groups: []duplicate_functions.Group{{Digest: duplicate_functions.Digest{1}, Size: 12,
				Members: []duplicate_functions.Function{
					{File: "/project/a.go", Line: 3, Name: "A", Size: 12, Digest: duplicate_functions.Digest{1},
						Kept: "port of upstream"},
				}}},
			Withheld: 1, BelowFloor: 2,
			Refused: []duplicate_functions.Refusal{{File: "/project/b.go", Line: 4, Name: "B",
				Reason: duplicate_functions.RefusalUnsupportedInstruction, Detail: "*ir.Select"}},
			NotRead:             map[duplicate_functions.NotReadReason]int{duplicate_functions.NotReadGenerated: 1},
			Crashes:             []duplicate_functions.Crash{{File: "/project/c.go", Error: "a panic"}},
			DeadSuppressions:    []duplicate_functions.Place{{File: "/project/a.go", Line: 9}},
			ElapsedMilliseconds: 7,
			Cache:               duplicate_functions.CacheUse{Unit: "packages", Read: 1, Hashed: 1},
		}},
		MemoryRecord{RaisedToBytes: []int64{9663676416}},
		InputsRecord{
			Files:          []string{"/.git/index", "/project/a.go", "/project/go.mod"},
			Directories:    []string{"/project"},
			Absent:         []string{"/go.work"},
			TrackedIgnored: []TrackedIgnoredScope{{Index: "/.git/index", Root: "/", Scope: "project"}},
		},
		TimingRecord{
			Unavailable:            "this platform has no per-thread CPU clock",
			ExtraBuildsFromSource:  2,
			ProcessCpuMicroseconds: 80000,
			Load: LoadTiming{
				ListCpuMicroseconds:         2000,
				ListChildrenCpuMicroseconds: 150000,
				ParseCpuMicroseconds:        4000,
				ParsedFiles:                 24,
				TypeCheckCpuMicroseconds:    12000,
				TypeCheckedPackages:         3,
				ExportDataCpuMicroseconds:   6000,
				ExportDataPackages:          37,
			},
			Rules: []RuleTiming{{Name: "cohere-go/printf", CpuMicroseconds: 2500, Packages: 3, Calls: 640}},
			Shared: []SharedTiming{
				{
					Name:            "build_intermediate_representation",
					Kind:            SharedIntermediateRepresentation,
					CpuMicroseconds: 8000,
					Packages:        40,
				},
			},
			MeasuredCalls:            2300,
			MeasuringCpuMicroseconds: 230,
		},
		SummaryRecord{Findings: 1, Complete: true, ExitCode: 1, UnguardedWorkspaceSums: []string{"/work/go.work.sum"}},
	}
}

// TestEveryRecordWritesTheContractsFields writes one fully populated record of each kind through the
// Writer, decodes each line with encoding/json into a map, and compares its field names, at every
// depth, against the fields the contract shows for that kind. The front door's own decoding lives in
// package main, which no test outside it can import, so the contract's examples and fixtures, which the
// front door's tests read too, are the reference.
func TestEveryRecordWritesTheContractsFields(test *testing.T) {
	test.Parallel()
	shapes, fieldIndex := contractShapes(test)

	var output bytes.Buffer
	writer := NewWriter(&output)
	records := populatedRecords()
	for _, record := range records {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatalf("%s: %v", record.kind(), writeError)
		}
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != len(records) {
		test.Fatalf("%d records wrote %d lines", len(records), len(lines))
	}

	written := map[Kind]bool{}
	for index, record := range records {
		object := json.RawMessage(lines[index])
		if !json.Valid(object) {
			test.Fatalf("%s wrote a line that is not JSON\n%s", record.kind(), lines[index])
		}
		if kind, _ := kindOf(object); kind != record.kind() {
			test.Errorf("a %s record wrote kind %q", record.kind(), kind)
		}
		written[record.kind()] = true
		ours := shapeOf(test, "", object)
		delete(ours, "kind")
		theirs, inContract := shapes[record.kind()]
		if !inContract {
			test.Errorf("%s is written and the contract shows no such record", record.kind())
			continue
		}
		// An additive field the contract does not show yet is left out of the comparison. One it shows, as the Swift
		// engine's NotWritten.jsonl shows `notWritten`, is compared like any other (#723rzxm).
		for _, additive := range additiveFields[record.kind()] {
			if _, present := ours[additive]; !present {
				test.Errorf("%s does not write its additive field %s", record.kind(), additive)
			}
			if _, documented := theirs[additive]; !documented {
				delete(ours, additive)
			}
		}
		for _, problem := range compareShapes(
			string(record.kind()),
			ours,
			theirs,
			fieldIndex,
			fieldsNotWritten[record.kind()],
		) {
			test.Error(problem)
		}
	}

	for kind := range shapes {
		_, onPurpose := kindsNotWritten[kind]
		if !written[kind] && !onPurpose {
			test.Errorf("the contract shows a %q record and this package has no type for it", kind)
		}
		if written[kind] && onPurpose {
			test.Errorf("%q is written and is listed as not written", kind)
		}
	}
}

// TestFixtureRecordsReencodeToTheSameJson decodes every fixture line into its record type and writes it
// back through one Writer per fixture, so every name and value matches the stream the front door's tests
// pin, and each fixture's order passes the Writer's refusals. The comparison is of decoded values, not
// bytes: the fixtures captured from real Swift runs have their keys sorted by Swift's encoder, and key
// order is not part of JSON. Provenance is exempt, since it leaves out the Swift library versions on
// purpose, and so is a fix record from before `changedFiles`, which this engine always writes.
func TestFixtureRecordsReencodeToTheSameJson(test *testing.T) {
	test.Parallel()
	newRecord := map[Kind]func() Record{
		KindProvenance: func() Record { return &ProvenanceRecord{} },
		KindProject:    func() Record { return &ProjectRecord{} },
		KindUnreadable: func() Record { return &UnreadableRecord{} },
		KindFinding:    func() Record { return &FindingRecord{} },
		KindFix:        func() Record { return &FixRecord{} },
		KindTypes:      func() Record { return &TypesRecord{} },
		KindLint:       func() Record { return &LintRecord{} },
		KindPhase:      func() Record { return &PhaseRecord{} },
		KindRule:       func() Record { return &RuleRecord{} },
		KindMemory:     func() Record { return &MemoryRecord{} },
		KindInputs:     func() Record { return &InputsRecord{} },
		KindTiming:     func() Record { return &TimingRecord{} },
		KindSummary:    func() Record { return &SummaryRecord{} },
	}
	fixtures, globError := filepath.Glob(contractFixtures)
	if globError != nil || len(fixtures) == 0 {
		test.Fatalf("no contract fixtures at %s (%v)", contractFixtures, globError)
	}
	compared := 0
	for _, fixture := range fixtures {
		contents, readError := os.ReadFile(fixture)
		if readError != nil {
			test.Fatal(readError)
		}
		var output bytes.Buffer
		writer := NewWriter(&output)
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			var envelope struct {
				Kind Kind `json:"kind"`
			}
			if decodeError := json.Unmarshal([]byte(line), &envelope); decodeError != nil {
				test.Fatalf("%s: %v", fixture, decodeError)
			}
			if _, onPurpose := kindsNotWritten[envelope.Kind]; onPurpose {
				continue
			}
			newRecordOfKind, known := newRecord[envelope.Kind]
			if !known {
				test.Fatalf(
					"%s: a %q record, which this package has no type for",
					filepath.Base(fixture),
					envelope.Kind,
				)
			}
			record := newRecordOfKind()
			if decodeError := json.Unmarshal([]byte(line), record); decodeError != nil {
				test.Fatalf("%s: %v\n%s", fixture, decodeError, line)
			}
			output.Reset()
			if writeError := writer.Write(record); writeError != nil {
				test.Fatalf("%s: %v", filepath.Base(fixture), writeError)
			}
			if envelope.Kind == KindProvenance ||
				(envelope.Kind == KindFix && !strings.Contains(line, `"changedFiles"`)) {
				continue
			}
			compared++
			// Every record is a JSON object, so each side decodes into one.
			reencoded := map[string]any{}
			original := map[string]any{}
			if decodeError := json.Unmarshal(output.Bytes(), &reencoded); decodeError != nil {
				test.Fatalf(
					"%s: the reencoded %s record does not decode: %v\n%s",
					filepath.Base(fixture),
					envelope.Kind,
					decodeError,
					output.String(),
				)
			}
			if decodeError := json.Unmarshal([]byte(line), &original); decodeError != nil {
				test.Fatal(decodeError)
			}
			// A fixture from before an additive field has none, and this engine always writes it.
			for _, additive := range additiveFields[envelope.Kind] {
				if !strings.Contains(line, `"`+additive+`"`) {
					delete(reencoded, additive)
				}
			}
			if !reflect.DeepEqual(reencoded, original) {
				test.Errorf(
					"%s: a %s record reencodes differently\n got: %s want: %s",
					filepath.Base(fixture),
					envelope.Kind,
					output.String(),
					line,
				)
			}
		}
	}
	if compared == 0 {
		test.Error("no fixture line was compared")
	}
}

// TestLintRecordWritesEmptyWithheldCountsAsObjects keeps an unset count from encoding as null, which would read
// as "not counted" rather than "none withheld".
func TestLintRecordWritesEmptyWithheldCountsAsObjects(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, LintRecord{}} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for _, field := range []string{`"withheldBySuppression":{}`, `"withheldAsGenerated":{}`, `"withheldAsCopied":{}`} {
		if !strings.Contains(output.String(), field) {
			test.Errorf("an empty lint record wrote %s, want %s in it", output.String(), field)
		}
	}
}

// TestLintRecordWritesBuildsOnlyForMoreThanOne keeps a run of one build writing the lint record it always has, with no
// builds key, and a run of several naming each build (#72n9mq0).
func TestLintRecordWritesBuildsOnlyForMoreThanOne(test *testing.T) {
	test.Parallel()
	encode := func(record LintRecord) string {
		encoded, encodeError := json.Marshal(record)
		if encodeError != nil {
			test.Fatal(encodeError)
		}
		return string(encoded)
	}
	if one := encode(LintRecord{}); strings.Contains(one, `"builds"`) {
		test.Errorf("a run of one build wrote %s, with a builds key", one)
	}
	several := encode(LintRecord{Builds: []BuildRecord{
		{Target: "darwin/arm64 cgo", Host: true, CompiledFiles: 3, AnalyzedPackages: 2},
		{Target: "windows/arm64", CompiledFiles: 3, AnalyzedPackages: 1, FindingsAdded: 2, FindingsDropped: 1},
	}})
	for _, field := range []string{
		`"builds":[{"target":"darwin/arm64 cgo","host":true,"compiledFiles":3,"analyzedPackages":2,` +
			`"findingsAdded":0,"findingsDropped":0},`,
		`{"target":"windows/arm64","host":false,"compiledFiles":3,"analyzedPackages":1,"findingsAdded":2,` +
			`"findingsDropped":1}]`,
	} {
		if !strings.Contains(several, field) {
			test.Errorf("a run of two builds wrote %s, want %s in it", several, field)
		}
	}
}

// TestTypesRecordWritesNoWithheldFilesAsAnEmptyList keeps an unset list from encoding as null, which would read as
// "not counted" rather than "none withheld".
func TestTypesRecordWritesNoWithheldFilesAsAnEmptyList(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, TypesRecord{}} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if !strings.Contains(output.String(), `"withheldInDependencyCgo":[]`) {
		test.Errorf("an empty types record wrote %s, want %s in it", output.String(), `"withheldInDependencyCgo":[]`)
	}
}

// TestInputsRecordWritesEmptyListsAsLists keeps an unset list from encoding as null, which would read as "not
// recorded" rather than "nothing read".
func TestInputsRecordWritesEmptyListsAsLists(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, InputsRecord{}} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for _, field := range []string{`"files":[]`, `"directories":[]`, `"absent":[]`, `"trackedIgnored":[]`} {
		if !strings.Contains(output.String(), field) {
			test.Errorf("an empty inputs record wrote %s, want %s in it", output.String(), field)
		}
	}
}

// TestInputsRecordRoundTrips decodes a written inputs record and writes it again, so the front door, which decodes
// the line into its own type, reads back every path the engine declared, in order.
func TestInputsRecordRoundTrips(test *testing.T) {
	test.Parallel()
	written := InputsRecord{
		Files:       []string{"/goroot/VERSION", "/project/a.go", "/project/go.mod"},
		Directories: []string{"/project", "/project/nested"},
		Absent:      []string{"/go.work", "/project/go.sum"},
		TrackedIgnored: []TrackedIgnoredScope{
			{Index: "/repository/.git/index", Root: "/repository", Scope: "project"},
		},
	}
	var first bytes.Buffer
	writer := NewWriter(&first)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, written} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	lines := strings.Split(strings.TrimSuffix(first.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], `{"kind":"inputs",`) {
		test.Fatalf("want a provenance line and an inputs line, got:\n%s", first.String())
	}
	var decoded InputsRecord
	if decodeError := json.Unmarshal([]byte(lines[1]), &decoded); decodeError != nil {
		test.Fatal(decodeError)
	}
	if !reflect.DeepEqual(decoded, written) {
		test.Errorf("decoded %+v, want %+v", decoded, written)
	}
	var second bytes.Buffer
	rewriter := NewWriter(&second)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, decoded} {
		if writeError := rewriter.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if second.String() != first.String() {
		test.Errorf("the decoded record wrote\n%s\nwant\n%s", second.String(), first.String())
	}
}

// TestMemoryRecordWritesNoRaisesAsAnEmptyList keeps a run that raised nothing from writing null, which would read as
// "not recorded" rather than "nothing raised".
func TestMemoryRecordWritesNoRaisesAsAnEmptyList(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{ProvenanceRecord{Contract: Version}, MemoryRecord{}} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if want := `{"kind":"memory","raisedToBytes":[]}` + "\n"; !strings.HasSuffix(output.String(), want) {
		test.Errorf("a memory record with no raises wrote %s, want it to end %s", output.String(), want)
	}
}

// TestMemoryRecordRoundTrips decodes a written memory record and writes it again, so the front door, which decodes the
// line into its own type, reads back every limit the run raised to, in order, and an empty list as an empty list.
func TestMemoryRecordRoundTrips(test *testing.T) {
	test.Parallel()
	for _, written := range []MemoryRecord{
		{RaisedToBytes: []int64{9663676416, 9663676416}},
		{RaisedToBytes: []int64{}},
	} {
		var first bytes.Buffer
		writer := NewWriter(&first)
		for _, record := range []Record{ProvenanceRecord{Contract: Version}, written} {
			if writeError := writer.Write(record); writeError != nil {
				test.Fatal(writeError)
			}
		}
		lines := strings.Split(strings.TrimSuffix(first.String(), "\n"), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[1], `{"kind":"memory","raisedToBytes":[`) {
			test.Fatalf("want a provenance line and a memory line, got:\n%s", first.String())
		}
		var decoded MemoryRecord
		if decodeError := json.Unmarshal([]byte(lines[1]), &decoded); decodeError != nil {
			test.Fatal(decodeError)
		}
		if !reflect.DeepEqual(decoded, written) {
			test.Errorf("decoded %+v, want %+v", decoded, written)
		}
		var second bytes.Buffer
		rewriter := NewWriter(&second)
		for _, record := range []Record{ProvenanceRecord{Contract: Version}, decoded} {
			if writeError := rewriter.Write(record); writeError != nil {
				test.Fatal(writeError)
			}
		}
		if second.String() != first.String() {
			test.Errorf("the decoded record wrote\n%s\nwant\n%s", second.String(), first.String())
		}
	}
}
