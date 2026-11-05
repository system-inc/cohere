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
// comment says why.
var fieldsNotWritten = map[Kind][]string{
	KindProvenance: {"swiftSyntax", "swiftFormat"},
}

// dictionaryFields are objects keyed by data (a reason, a rule name) rather than by field names, so their
// keys are values and are not compared.
var dictionaryFields = map[string]bool{
	"refusalsByReason":    true,
	"notFormattedReasons": true,
	"fixedBy":             true,
	"rulesScopedOff":      true,
	"filesNotChecked":     true,
	"skipped":             true,
}

// shape is a JSON object's field names, each with the shape of its value: nil for a value with no fields,
// and for an array the merged shape of its elements.
type shape map[string]shape

func shapeOf(name string, value any) shape {
	if dictionaryFields[name] {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		fields := shape{}
		for key, member := range typed {
			fields[key] = mergeShapes(fields[key], shapeOf(key, member))
		}
		return fields
	case []any:
		var elements shape
		for _, element := range typed {
			elements = mergeShapes(elements, shapeOf(name, element))
		}
		return elements
	}
	return nil
}

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
func contractShapes(t *testing.T) (map[Kind]shape, map[string]shape) {
	t.Helper()
	objects := []map[string]any{}

	document, readError := os.ReadFile(contractDocument)
	if readError != nil {
		t.Fatal(readError)
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
				object := map[string]any{}
				decodeError := decoder.Decode(&object)
				if errors.Is(decodeError, io.EOF) {
					break
				}
				if decodeError != nil {
					t.Fatalf("a JSON example in Contract.md does not parse: %v\n%s", decodeError, block.String())
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
		t.Fatalf("no contract fixtures at %s (%v)", contractFixtures, globError)
	}
	for _, fixture := range fixtures {
		contents, readError := os.ReadFile(fixture)
		if readError != nil {
			t.Fatal(readError)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			object := map[string]any{}
			if decodeError := json.Unmarshal([]byte(line), &object); decodeError != nil {
				t.Fatalf("%s: %v\n%s", fixture, decodeError, line)
			}
			objects = append(objects, object)
		}
	}

	shapes := map[Kind]shape{}
	fieldIndex := map[string]shape{}
	for _, object := range objects {
		kind, isString := object["kind"].(string)
		if !isString {
			t.Fatalf("a contract example has no kind: %v", object)
		}
		fields := shapeOf("", object)
		delete(fields, "kind")
		shapes[Kind(kind)] = mergeShapes(shapes[Kind(kind)], fields)
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
		ProvenanceRecord{Contract: Version, Engine: "cohere-go", Version: "0.1.0", Commit: "dev", SourceTreeModified: true, Toolchain: "go1.27"},
		ProjectRecord{Root: "/project", Package: "example.com/project", ElapsedMilliseconds: 4, FilesInPackage: 3, FilesOurs: 2,
			FilesInScope: 2, ScopeDescription: "internal/a", Targets: []ProjectTarget{{Name: "example.com/project", Kind: "module", Files: 3, LanguageMode: "1.27"}},
			Excluded: []ExcludedFile{{File: "/project/b_windows.go", Reason: "excluded by build constraints on darwin/arm64"}}},
		UnreadableRecord{File: "/project/c.go", Error: "it could not be opened"},
		FindingRecord{Source: SourceRule, File: "/project/a.go", Line: 2, Column: 9, EndLine: 2, EndColumn: 21, Severity: SeverityError,
			Rule: "cohere-go/printf", MessageId: "printf", Message: "a message", Fixes: []Edit{edit},
			Suggestions: []Suggestion{{Message: "a suggestion", Fixes: []Edit{edit}}}},
		FixRecord{FilesConsidered: 2, FilesRewritten: 1, FixesApplied: 1, FixesRefused: 1, RefusalsByReason: map[string]int{"overlaps another fix": 1},
			FilesReformatted: 1, FilesNotFormatted: 1, NotFormattedReasons: map[string]int{"it does not parse": 1}, FormatScope: "every file in scope",
			ChangedFiles: []ChangedFile{{File: "/project/a.go", FixedBy: map[string]int{"cohere-go/printf": 1}, Formatted: true}}},
		TypesRecord{Diagnostics: 1, Files: 2, ElapsedMilliseconds: 900, FilesWithoutRecord: []string{"/project/d.go"}, Build: "go list -deps -test"},
		LintRecord{Findings: 1, RulesRun: 4, FilesWalked: 2, NodesVisited: 640, ElapsedMilliseconds: 12, ReusedFrom: "fix",
			RulesSilent: []string{"cohere-go/copylock"}, RulesWatchedAndQuiet: 2, Crashes: []Crash{{File: "/project/a.go", Error: "a panic"}},
			RulesScopedOff: map[string]int{"cohere-go/printf": 1}, RulesNotConfigured: []string{"cohere-go/SA4006"}, ConfigNote: "a note"},
		PhaseRecord{Name: PhaseLint, Outcome: OutcomeRan, ElapsedMilliseconds: 12, Detail: "a detail"},
		RuleRecord{Name: "cohere-go/printf", Severity: "error"},
		SummaryRecord{Findings: 1, Complete: true, ExitCode: 1},
	}
}

// TestEveryRecordWritesTheContractsFields writes one fully populated record of each kind through the
// Writer, decodes each line with encoding/json into a map, and compares its field names, at every
// depth, against the fields the contract shows for that kind. The front door's own decoding lives in
// package main, which no test outside it can import, so the contract's examples and fixtures, which the
// front door's tests read too, are the reference.
func TestEveryRecordWritesTheContractsFields(t *testing.T) {
	t.Parallel()
	shapes, fieldIndex := contractShapes(t)

	var output bytes.Buffer
	writer := NewWriter(&output)
	records := populatedRecords()
	for _, record := range records {
		if writeError := writer.Write(record); writeError != nil {
			t.Fatalf("%s: %v", record.kind(), writeError)
		}
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != len(records) {
		t.Fatalf("%d records wrote %d lines", len(records), len(lines))
	}

	written := map[Kind]bool{}
	for index, record := range records {
		object := map[string]any{}
		if decodeError := json.Unmarshal([]byte(lines[index]), &object); decodeError != nil {
			t.Fatalf("%s: %v\n%s", record.kind(), decodeError, lines[index])
		}
		if object["kind"] != string(record.kind()) {
			t.Errorf("a %s record wrote kind %v", record.kind(), object["kind"])
		}
		written[record.kind()] = true
		ours := shapeOf("", object)
		delete(ours, "kind")
		theirs, inContract := shapes[record.kind()]
		if !inContract {
			t.Errorf("%s is written and the contract shows no such record", record.kind())
			continue
		}
		for _, problem := range compareShapes(string(record.kind()), ours, theirs, fieldIndex, fieldsNotWritten[record.kind()]) {
			t.Error(problem)
		}
	}

	for kind := range shapes {
		_, onPurpose := kindsNotWritten[kind]
		if !written[kind] && !onPurpose {
			t.Errorf("the contract shows a %q record and this package has no type for it", kind)
		}
		if written[kind] && onPurpose {
			t.Errorf("%q is written and is listed as not written", kind)
		}
	}
}

// TestFixtureRecordsReencodeToTheSameJSON decodes every fixture line into its record type and writes it
// back through one Writer per fixture, so every name and value matches the stream the front door's tests
// pin, and each fixture's order passes the Writer's refusals. The comparison is of decoded values, not
// bytes: the fixtures captured from real Swift runs have their keys sorted by Swift's encoder, and key
// order is not part of JSON. Provenance is exempt, since it leaves out the Swift library versions on
// purpose, and so is a fix record from before `changedFiles`, which this engine always writes.
func TestFixtureRecordsReencodeToTheSameJSON(t *testing.T) {
	t.Parallel()
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
		KindSummary:    func() Record { return &SummaryRecord{} },
	}
	fixtures, globError := filepath.Glob(contractFixtures)
	if globError != nil || len(fixtures) == 0 {
		t.Fatalf("no contract fixtures at %s (%v)", contractFixtures, globError)
	}
	compared := 0
	for _, fixture := range fixtures {
		contents, readError := os.ReadFile(fixture)
		if readError != nil {
			t.Fatal(readError)
		}
		var output bytes.Buffer
		writer := NewWriter(&output)
		for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
			var envelope struct {
				Kind Kind `json:"kind"`
			}
			if decodeError := json.Unmarshal([]byte(line), &envelope); decodeError != nil {
				t.Fatalf("%s: %v", fixture, decodeError)
			}
			if _, onPurpose := kindsNotWritten[envelope.Kind]; onPurpose {
				continue
			}
			newRecordOfKind, known := newRecord[envelope.Kind]
			if !known {
				t.Fatalf("%s: a %q record, which this package has no type for", filepath.Base(fixture), envelope.Kind)
			}
			record := newRecordOfKind()
			if decodeError := json.Unmarshal([]byte(line), record); decodeError != nil {
				t.Fatalf("%s: %v\n%s", fixture, decodeError, line)
			}
			output.Reset()
			if writeError := writer.Write(record); writeError != nil {
				t.Fatalf("%s: %v", filepath.Base(fixture), writeError)
			}
			if envelope.Kind == KindProvenance || (envelope.Kind == KindFix && !strings.Contains(line, `"changedFiles"`)) {
				continue
			}
			compared++
			var reencoded, original any
			if decodeError := json.Unmarshal(output.Bytes(), &reencoded); decodeError != nil {
				t.Fatalf("%s: the reencoded %s record does not decode: %v\n%s", filepath.Base(fixture), envelope.Kind, decodeError, output.String())
			}
			if decodeError := json.Unmarshal([]byte(line), &original); decodeError != nil {
				t.Fatal(decodeError)
			}
			if !reflect.DeepEqual(reencoded, original) {
				t.Errorf("%s: a %s record reencodes differently\n got: %s want: %s", filepath.Base(fixture), envelope.Kind, output.String(), line)
			}
		}
	}
	if compared == 0 {
		t.Error("no fixture line was compared")
	}
}
