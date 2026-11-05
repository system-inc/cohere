package rule_testing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/shim/scanner"
	"github.com/system-inc/cohere/internal/docsdata/capture"
	"github.com/system-inc/cohere/internal/lint/rule"
)

// capturedRun is what a Run hands the docs capture, kept on its Result so the assertion that passes on
// it can record the case. Nil unless COHERE_DOCS_CAPTURE is set, which is every ordinary run.
//
// The website's examples are the cases these tests assert, rather than examples written for the site,
// so an example can only show what a passing test has shown (internal/docsdata).
type capturedRun struct {
	rule       string
	file       string
	otherFiles int

	// options is the options the case was run with, as JSON, nil for a rule run unconfigured. optionsError is
	// why they did not encode, reported when the case is recorded.
	options      []byte
	optionsError error
}

// newCapturedRun starts a record for a Run, or returns nil when capture is off.
func newCapturedRun[Options any](
	ruleName string,
	fileName string,
	otherFiles int,
	options rule.Given[Options],
) *capturedRun {
	if capture.Directory() == "" {
		return nil
	}
	captured := &capturedRun{rule: ruleName, file: fileName, otherFiles: otherFiles}
	// Encoded here, while the options' type is known, so the record holds bytes rather than an untyped value.
	if value, configured := options.Read(); configured {
		captured.options, captured.optionsError = json.Marshal(value)
	}
	return captured
}

// RecordAssertedCase records a case a test has just asserted by its own comparison rather than through
// an Expect helper: a replayed upstream corpus that checks each finding's span, text and message, which
// ExpectFindings cannot express. Call it only after that comparison passed. Without it the docs capture
// never sees the case, and rules.json never learns a message id only such a corpus asserts.
func RecordAssertedCase(test *testing.T, result Result) {
	test.Helper()
	outcome := capture.OutcomeFindings
	if len(result.Diagnostics) == 0 {
		outcome = capture.OutcomeClean
	}
	recordCase(test, result, outcome, "")
}

// recordCase writes the case an assertion just passed on. Called at the end of each Expect, after every
// check, so only an asserted outcome is ever recorded.
func recordCase(test *testing.T, result Result, outcome string, fixedSource string) {
	test.Helper()
	if result.capture == nil || result.SourceFile == nil {
		return
	}
	record := capture.Record{
		Rule:        result.capture.rule,
		File:        result.capture.file,
		Source:      result.SourceFile.Text(),
		OtherFiles:  result.capture.otherFiles,
		Outcome:     outcome,
		FixedSource: fixedSource,
	}
	if result.capture.optionsError != nil {
		// A case whose options cannot be shown cannot be an example either; the test itself passed.
		test.Logf("docs capture: skipping a case whose options do not encode: %v", result.capture.optionsError)
		return
	}
	if result.capture.options != nil {
		record.Options = result.capture.options
	}
	for _, diagnostic := range result.Diagnostics {
		sourceFile := result.SourceFile
		if diagnostic.SourceFile != nil {
			sourceFile = diagnostic.SourceFile
		}
		// Positioned as cohere prints a finding: one-based, the column a byte offset into the line.
		line, column := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, diagnostic.Range.Pos())
		endLine, endColumn := scanner.GetECMALineAndByteOffsetOfPosition(sourceFile, diagnostic.Range.End())
		record.Findings = append(record.Findings, capture.Finding{
			Line:        line + 1,
			Column:      column + 1,
			EndLine:     endLine + 1,
			EndColumn:   endColumn + 1,
			MessageId:   diagnostic.Message.Id,
			Message:     strings.ReplaceAll(diagnostic.Message.Description, "\n", " "),
			Fix:         len(diagnostic.Fixes) > 0,
			Suggestions: len(diagnostic.Suggestions),
		})
	}
	if writeError := capture.Write(record); writeError != nil {
		test.Fatalf("docs capture: %v", writeError)
	}
}
