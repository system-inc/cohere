package contract

import (
	"bytes"
	"strings"
	"testing"
)

// provenanceForTest is a provenance record any test stream can open with.
var provenanceForTest = ProvenanceRecord{
	Contract:  Version,
	Engine:    "cohere-go",
	Version:   "0.1.0",
	Commit:    "dev",
	Toolchain: "go1.27",
}

// TestWriterRefusesARecordBeforeProvenance holds the first line of every stream to provenance.
func TestWriterRefusesARecordBeforeProvenance(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	writeError := writer.Write(SummaryRecord{Findings: 0, Complete: true, ExitCode: 0})
	if writeError == nil || !strings.Contains(writeError.Error(), "before provenance") {
		test.Fatalf("a summary as the first record: got %v, expected a refusal naming provenance", writeError)
	}
	if output.Len() != 0 {
		test.Errorf("a refused record wrote %q", output.String())
	}
	// The refusal leaves the writer able to start the stream properly.
	if writeError := writer.Write(provenanceForTest); writeError != nil {
		test.Fatalf("provenance after a refused record: %v", writeError)
	}
}

// TestWriterRefusesASecondProvenance holds a stream to one provenance, as the front door does.
func TestWriterRefusesASecondProvenance(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	if writeError := writer.Write(provenanceForTest); writeError != nil {
		test.Fatal(writeError)
	}
	written := output.Len()
	if writeError := writer.Write(provenanceForTest); writeError == nil {
		test.Fatal("a second provenance record was accepted")
	}
	if output.Len() != written {
		test.Errorf("a refused record wrote %q", output.String()[written:])
	}
}

// TestWriterRefusesEveryRecordAfterTheSummary holds the summary to last, for each kind that could follow.
func TestWriterRefusesEveryRecordAfterTheSummary(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{
		provenanceForTest,
		PhaseRecord{Name: PhaseFix, Outcome: OutcomeRan},
		SummaryRecord{Complete: true},
	} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatalf("%s: %v", record.kind(), writeError)
		}
	}
	written := output.Len()
	after := []Record{
		ProvenanceRecord{}, ProjectRecord{}, UnreadableRecord{}, FindingRecord{}, FixRecord{}, TypesRecord{},
		LintRecord{}, PhaseRecord{}, RuleRecord{}, SummaryRecord{},
	}
	for _, record := range after {
		writeError := writer.Write(record)
		if writeError == nil || !strings.Contains(writeError.Error(), "after the summary") {
			test.Errorf("a %s record after the summary: got %v, expected a refusal", record.kind(), writeError)
		}
	}
	if output.Len() != written {
		test.Errorf("refused records wrote %q", output.String()[written:])
	}
}

// TestWriterWritesOneLinePerRecordWithKindFirst is the line shape the front door reads: one object per
// line, its kind first, a message's newline escaped rather than splitting the line, and nothing escaped
// that JSON outside HTML leaves alone.
func TestWriterWritesOneLinePerRecordWithKindFirst(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	records := []Record{
		provenanceForTest,
		FindingRecord{
			Source:    SourceRule,
			File:      "/project/a.go",
			Line:      1,
			Column:    1,
			Severity:  SeverityError,
			Rule:      "cohere-go/printf",
			MessageId: "printf",
			Message:   "a <- b & c\nsecond line",
		},
		SummaryRecord{Findings: 1, Complete: true, ExitCode: 1},
	}
	for _, record := range records {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatalf("%s: %v", record.kind(), writeError)
		}
	}
	lines := strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
	if len(lines) != len(records) {
		test.Fatalf("%d records wrote %d lines:\n%s", len(records), len(lines), output.String())
	}
	for index, record := range records {
		if !strings.HasPrefix(lines[index], `{"kind":"`+string(record.kind())+`",`) {
			test.Errorf("line %d does not open with its kind: %s", index, lines[index])
		}
	}
	if !strings.Contains(lines[1], `"message":"a <- b & c\nsecond line"`) {
		test.Errorf("the finding's message was escaped differently than expected: %s", lines[1])
	}
	if expected := `{"kind":"summary","findings":1,"complete":true,"exitCode":1}`; lines[2] != expected {
		test.Errorf("summary line\n got: %s\nwant: %s", lines[2], expected)
	}
}

// TestFixRecordListsNoChangedFilesAsAnEmptyList keeps an engine that wrote nothing from reading, at the
// front door, as one that did not say what it wrote.
func TestFixRecordListsNoChangedFilesAsAnEmptyList(test *testing.T) {
	test.Parallel()
	var output bytes.Buffer
	writer := NewWriter(&output)
	for _, record := range []Record{provenanceForTest, FixRecord{FormatScope: "every file"}} {
		if writeError := writer.Write(record); writeError != nil {
			test.Fatal(writeError)
		}
	}
	if !strings.Contains(output.String(), `"changedFiles":[]`) {
		test.Errorf("a fix record with no changed files wrote:\n%s", output.String())
	}
}
