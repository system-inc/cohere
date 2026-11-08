package main

import (
	"strings"
	"testing"
)

// A Swift project whose every file lacked a compiler record, so the type check examined none and lint never ran,
// reads no checked count in its footer: before, the front door preset checked to the files in scope, and the line
// read `1 type error (2 checked) • ⚠ 2 files nothing checked` (#1hva1ya). Checked is what a phase examined.
// Not parallel: it renders a check run through engineRun, whose record acceptors set the package-level activeSummary.
func TestAFooterCountsOnlyTheFilesAPhaseExamined(test *testing.T) {
	records := contractFixture(test, "TypesBail.jsonl")
	for index, record := range records {
		if strings.Contains(record, `"kind":"types"`) {
			withoutRecord := `"/project/Sources/Example/Broken.swift","/project/Sources/Example/Other.swift"`
			records[index] = strings.Replace(record, `"filesWithoutRecord":[]`,
				`"filesWithoutRecord":[`+withoutRecord+`]`, 1)
		}
	}
	activeSummary = runSummary{}
	output, _, renderRecordsError := renderRecords(test, engineModeCheck, records, 1)
	if renderRecordsError != nil {
		test.Fatalf("%v\n%s", renderRecordsError, output)
	}
	// The footer the run printed, its last line.
	lines := strings.Split(strings.TrimSpace(output), "\n")
	line := lines[len(lines)-1]
	if activeSummary.FilesChecked != 0 || strings.Contains(line, "2 checked") {
		test.Errorf("a run whose files no phase examined counts %d checked: %s", activeSummary.FilesChecked, line)
	}
	if !strings.Contains(line, "⚠ 2 files nothing checked") || !strings.Contains(line, "lint did not run") {
		test.Errorf("the line lost the gaps that name what nothing checked: %s", line)
	}

	// The same stream with the records present: the compiler examined both files, so both are checked.
	activeSummary = runSummary{}
	present := contractFixture(test, "TypesBail.jsonl")
	if _, _, renderRecordsError := renderRecords(test, engineModeCheck, present, 1); renderRecordsError != nil {
		test.Fatal(renderRecordsError)
	}
	if activeSummary.FilesChecked != 2 {
		test.Errorf("a type check that examined both files counts %d checked, want 2", activeSummary.FilesChecked)
	}
}
