package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/system-inc/cohere/internal/docsdata"
	"github.com/system-inc/cohere/internal/docsdata/capture"
)

// moduleRoot is the module's root: go test runs in this package's directory, four below it.
const moduleRoot = "../../../.."

// moduleCopy copies every file a check reads into a fresh directory: docs/data, and the sources beside it
// that SourceInputs reads. A test plants a change in the copy and never touches the module's own files.
func moduleCopy(test *testing.T) string {
	test.Helper()
	root := test.TempDir()
	copyFile := func(relative string) {
		contents, readError := os.ReadFile(filepath.Join(moduleRoot, relative))
		if readError != nil {
			test.Fatal(readError)
		}
		destination := filepath.Join(root, relative)
		if createDirectoryError := os.MkdirAll(filepath.Dir(destination), 0o755); createDirectoryError != nil {
			test.Fatal(createDirectoryError)
		}
		if writeError := os.WriteFile(destination, contents, 0o644); writeError != nil {
			test.Fatal(writeError)
		}
	}
	for _, relative := range []string{"swift/HouseRuleVerdicts.json", "swift/Rules.json", "CHANGELOG.md"} {
		copyFile(relative)
	}
	records, err := filepath.Glob(filepath.Join(moduleRoot, "bench", "results", "*.json"))
	if err != nil {
		test.Fatal(err)
	}
	for _, record := range records {
		copyFile(filepath.Join("bench", "results", filepath.Base(record)))
	}
	err = filepath.WalkDir(filepath.Join(
		moduleRoot,
		docsdata.Directory,
	), func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(moduleRoot, path)
		if err != nil {
			return err
		}
		copyFile(relative)
		return nil
	})
	if err != nil {
		test.Fatal(err)
	}
	return root
}

// recordsReproducing is capture records from which PickExamples picks exactly these examples: the shown
// cases as they were asserted, and every other message id and fix kind as a case run with options, which
// counts and is never shown.
func recordsReproducing(examples docsdata.Examples) []capture.Record {
	var records []capture.Record
	if firing := examples.Firing; firing != nil {
		outcome := capture.OutcomeFindings
		if firing.FixedSource != "" {
			outcome = capture.OutcomeFixed
		}
		records = append(records, capture.Record{Rule: examples.Rule, File: firing.File, Source: firing.Source,
			Outcome: outcome, Findings: firing.Findings, FixedSource: firing.FixedSource})
	}
	if clean := examples.Clean; clean != nil {
		records = append(
			records,
			capture.Record{Rule: examples.Rule, File: clean.File, Source: clean.Source, Outcome: capture.OutcomeClean},
		)
	}
	suggestions := 0
	if examples.AssertsSuggestion {
		suggestions = 1
	}
	for _, id := range examples.AssertedMessageIds {
		records = append(
			records,
			capture.Record{
				Rule:     examples.Rule,
				File:     "options.ts",
				Source:   id,
				Options:  json.RawMessage(`{}`),
				Outcome:  capture.OutcomeFindings,
				Findings: []capture.Finding{{MessageId: id, Fix: examples.AssertsFix, Suggestions: suggestions}},
			},
		)
	}
	if len(records) == 0 {
		records = append(records, capture.Record{
			Rule:    examples.Rule,
			File:    "options.ts",
			Options: json.RawMessage(`{}`),
			Outcome: capture.OutcomeClean,
		})
	}
	return records
}

// lintTestsAsCommitted is a capture whose lint tests assert exactly the module's committed examples: what
// the real capture records when the committed examples are current.
func lintTestsAsCommitted(test *testing.T) func(directory string) error {
	test.Helper()
	committed, readCommittedExamplesError := docsdata.ReadCommittedExamples(moduleRoot)
	if readCommittedExamplesError != nil {
		test.Fatal(readCommittedExamplesError)
	}
	if len(committed) == 0 {
		test.Fatal("the module has no committed examples, so a check against them would prove nothing")
	}
	return func(directory string) error {
		if createDirectoryError := os.MkdirAll(directory, 0o755); createDirectoryError != nil {
			return createDirectoryError
		}
		var lines []string
		for _, examples := range committed {
			for _, record := range recordsReproducing(examples) {
				encoded, encodeError := json.Marshal(record)
				if encodeError != nil {
					return encodeError
				}
				lines = append(lines, string(encoded))
			}
		}
		sort.Strings(lines)
		return os.WriteFile(filepath.Join(directory, "capture-1.jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
}

// noHelp leaves cli.json out of the check, since building the binary is not what this test is about.
func noHelp(string) ([]byte, map[string][]byte, error) { return nil, nil, nil }

// plantStaleExample rewrites one committed example's firing source in root, as an example looks when its
// rule's tests changed and nobody recaptured, and returns its path.
func plantStaleExample(test *testing.T, root string) string {
	test.Helper()
	committed, err := docsdata.ReadCommittedExamples(root)
	if err != nil {
		test.Fatal(err)
	}
	var names []string
	for name, examples := range committed {
		if examples.Firing != nil {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	planted := committed[names[0]]
	firing := *planted.Firing
	firing.Source += "// a case the rule's tests no longer assert\n"
	planted.Firing = &firing
	encoded, err := json.MarshalIndent(planted, "", "    ")
	if err != nil {
		test.Fatal(err)
	}
	path := docsdata.ExamplePath(planted.Rule)
	if writeError := os.WriteFile(filepath.Join(root, path), append(encoded, '\n'), 0o644); writeError != nil {
		test.Fatal(writeError)
	}
	return path
}

// TestCheckRecapturesTheExamples is #d7vx76v in both directions: -check passes a tree whose examples are
// what the lint tests assert, and names an example planted stale. The rendering depth, which reads the
// examples back, passes the planted tree, which is why it must never be the one called a check.
func TestCheckRecapturesTheExamples(test *testing.T) {
	test.Parallel()
	capturing := lintTestsAsCommitted(test)

	test.Run("a fresh tree passes -check", func(test *testing.T) {
		test.Parallel()
		root := moduleCopy(test)
		stale, staleFilesError := staleFiles(
			sources{root: root, help: noHelp, capture: capturing},
			test.TempDir(),
			true,
		)
		if staleFilesError != nil {
			test.Fatal(staleFilesError)
		}
		if len(stale) > 0 {
			test.Errorf("a copy of the module's own files is stale: %v", stale)
		}
	})

	test.Run("a planted stale example fails -check", func(test *testing.T) {
		test.Parallel()
		root := moduleCopy(test)
		planted := plantStaleExample(test, root)
		stale, staleFilesError := staleFiles(
			sources{root: root, help: noHelp, capture: capturing},
			test.TempDir(),
			true,
		)
		if staleFilesError != nil {
			test.Fatal(staleFilesError)
		}
		if !slices.Contains(stale, planted) {
			test.Errorf("-check missed the planted %s: %v", planted, stale)
		}
	})

	test.Run("the rendering depth cannot see a stale example", func(test *testing.T) {
		test.Parallel()
		root := moduleCopy(test)
		plantStaleExample(test, root)
		noCapture := func(string) error {
			test.Error("the rendering depth ran the lint tests")
			return nil
		}
		stale, staleFilesError := staleFiles(
			sources{root: root, help: noHelp, capture: noCapture},
			test.TempDir(),
			false,
		)
		if staleFilesError != nil {
			test.Fatal(staleFilesError)
		}
		if len(stale) > 0 {
			test.Errorf("the rendering depth reads the examples back, so it should find nothing stale here: %v", stale)
		}
	})

	test.Run("a capture that recorded nothing is refused", func(test *testing.T) {
		test.Parallel()
		recordsNothing := func(directory string) error { return os.MkdirAll(directory, 0o755) }
		_, staleFilesError := staleFiles(
			sources{root: moduleCopy(test), help: noHelp, capture: recordsNothing},
			test.TempDir(),
			true,
		)
		if staleFilesError == nil || !strings.Contains(staleFilesError.Error(), "recorded no case") {
			test.Errorf("an empty capture passed as a check: %v", staleFilesError)
		}
	})
}
